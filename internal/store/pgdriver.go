package store

// pgdriver.go — PostgreSQL 占位符翻译驱动。
//
// 背景:全库领域 SQL 统一使用 `?` 占位符(database/sql 通用写法,SQLite/MySQL 驱动均认)。
// 而 PostgreSQL(prepared statement)只认 `$1/$2/...` 位置参数。为免逐包重写数百条 SQL,
// 本文件实现一个 database/sql/driver 包装:把查询文本中的 `?` 顺序改写为 `$n` 后委托给
// lib/pq,对上层完全透明。
//
// 正确性要点:
//   - 改写器必须跳过 `?` 出现在字符串字面量/引号标识符/注释中的情况(本库 SQL 的 `?`
//     均为占位符,但防将来有人在 LIKE 模式里写 `?`)。
//   - 转发 pq 的 CheckNamedValue(uint64/数组归一)与 Ping(连接健康检查),
//     否则 database/sql 的默认检查器会拒掉 pq 支持的类型。
//   - lib/pq 只实现旧版 Queryer/Execer(无 Context 变体),故包装层实现
//     QueryerContext/ExecerContext 后把参数降级转发;database/sql 在调用前已经过
//     CheckNamedValue + 默认转换器把参数归一为 driver.Value,可直接透传。

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// PGDriverName 是包装驱动在 database/sql 中的注册名(区别于 pq 自注册的 "postgres")。
const PGDriverName = "atomgo-postgres"

func init() {
	sql.Register(PGDriverName, &pgDriver{inner: &pq.Driver{}})
}

// pgDriver 包装 pq.Driver:Open 出的连接经 pgConn 包装,查询文本中的 `?` 占位符
// 被顺序改写为 `n(PG 位置参数语法)。
type pgDriver struct{ inner driver.Driver }

func (d *pgDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &pgConn{Conn: c}, nil
}

// pgConn 在 pq 连接上做查询改写与接口转发。
type pgConn struct{ driver.Conn }

// QueryContext 实现 driver.QueryerContext:改写 `?` → `n 后委托 pq。
func (c *pgConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q := rewriteQuestionMarks(query)
	if inner, ok := c.Conn.(driver.QueryerContext); ok {
		return inner.QueryContext(ctx, q, args)
	}
	qr, ok := c.Conn.(driver.Queryer)
	if !ok {
		return nil, driver.ErrSkip // 让 database/sql 走 Prepare 路径
	}
	return qr.Query(q, namedValuesToValues(args))
}

// ExecContext 实现 driver.ExecerContext:改写 `?` → $n 后委托 pq。
func (c *pgConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	q := rewriteQuestionMarks(query)
	if inner, ok := c.Conn.(driver.ExecerContext); ok {
		return inner.ExecContext(ctx, q, args)
	}
	er, ok := c.Conn.(driver.Execer)
	if !ok {
		return nil, driver.ErrSkip // 让 database/sql 走 Prepare 路径
	}
	return er.Exec(q, namedValuesToValues(args))
}

// Prepare 改写查询文本后委托 pq(供 database/sql 的 Prepare 路径使用)。
func (c *pgConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(rewriteQuestionMarks(query))
}

// CheckNamedValue 转发 pq 的归一逻辑(uint64 → int64/string、pq.Array 等)。
func (c *pgConn) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// Ping 转发 pq 的连接探活(database/sql 凭 Pinger 接口做池健康检查)。
func (c *pgConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

// namedValuesToValues 把 database/sql 已归一的 NamedValue 降级为旧版 driver.Value 列表。
// database/sql 在调用 QueryerContext/ExecerContext 前已对每个参数应用过
// CheckNamedValue + 默认转换器,此处直接取值即可。
func namedValuesToValues(args []driver.NamedValue) []driver.Value {
	if len(args) == 0 {
		return nil
	}
	values := make([]driver.Value, len(args))
	for i, a := range args {
		values[i] = a.Value
	}
	return values
}

// rewriteQuestionMarks 把 SQL 文本中的 `?` 占位符顺序改写为 `1、$2、...。
//
// 扫描时跳过四类区域,确保其中的 `?` 原样保留:
//   - 单引号字符串 '…'(含 '' 转义)
//   - 双引号标识符 "…"
//   - 行注释 -- …(至行尾)
//   - 块注释 /* … */
//
// 无 `?` 时原样返回(零分配热路径)。本库 SQL 不在字符串内存入 `?`
// (已全量审计),改写是安全且确定性的。
func rewriteQuestionMarks(query string) string {
	if !strings.ContainsRune(query, '?') {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	var inSingle, inDouble, inLineComment, inBlockComment bool
	rs := []rune(query)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case inLineComment:
			b.WriteRune(c)
			if c == '\n' {
				inLineComment = false
			}
		case inBlockComment:
			b.WriteRune(c)
			if c == '*' && i+1 < len(rs) && rs[i+1] == '/' {
				b.WriteRune('/')
				i++
				inBlockComment = false
			}
		case inSingle:
			b.WriteRune(c)
			if c == '\'' {
				if i+1 < len(rs) && rs[i+1] == '\'' { // '' 转义:跳过下一个引号
					b.WriteRune('\'')
					i++
					continue
				}
				inSingle = false
			}
		case inDouble:
			b.WriteRune(c)
			if c == '"' {
				inDouble = false
			}
		default:
			switch c {
			case '\'':
				inSingle = true
				b.WriteRune(c)
			case '"':
				inDouble = true
				b.WriteRune(c)
			case '-':
				if i+1 < len(rs) && rs[i+1] == '-' {
					inLineComment = true
				}
				b.WriteRune(c)
			case '/':
				if i+1 < len(rs) && rs[i+1] == '*' {
					inBlockComment = true
				}
				b.WriteRune(c)
			case '?':
				n++
				b.WriteByte('$')
				b.WriteString(strconv.Itoa(n))
			default:
				b.WriteRune(c)
			}
		}
	}
	return b.String()
}
