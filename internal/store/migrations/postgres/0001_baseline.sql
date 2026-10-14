--
-- PostgreSQL database dump
--

\restrict VQmJ6gvYd4ww9p1b0c9bF3eeU4pzRP0n5Aj7EhWc7mpdJIXZGZbXhPbtSjcVADa

-- Dumped from database version 16.15
-- Dumped by pg_dump version 16.15

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: audit_log_no_delete(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION "public"."audit_log_no_delete"() RETURNS "trigger"
    LANGUAGE "plpgsql"
    AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: DELETE forbidden';
END;
$$;

--
-- Name: audit_log_no_update(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION "public"."audit_log_no_update"() RETURNS "trigger"
    LANGUAGE "plpgsql"
    AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: UPDATE forbidden';
END;
$$;

SET default_tablespace = '';

SET default_table_access_method = "heap";

--
-- Name: ai_config; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ai_config" (
    "id" integer NOT NULL,
    "provider" "text" DEFAULT ''::"text" NOT NULL,
    "base_url" "text" DEFAULT ''::"text" NOT NULL,
    "model" "text" DEFAULT ''::"text" NOT NULL,
    "api_key_ciphertext" "bytea",
    "budget_json" "text" DEFAULT '{"monthlyTokenLimit":null}'::"text" NOT NULL,
    "enabled" integer DEFAULT 0 NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL,
    CONSTRAINT "ai_config_id_check" CHECK (("id" = 1))
);

--
-- Name: anomaly_alerts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."anomaly_alerts" (
    "id" "text" NOT NULL,
    "server_id" "text" NOT NULL,
    "server_name" "text" NOT NULL,
    "metric" "text" NOT NULL,
    "operator" "text" NOT NULL,
    "threshold" real NOT NULL,
    "value" real NOT NULL,
    "message" "text" NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: anomaly_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."anomaly_rules" (
    "id" "text" NOT NULL,
    "metric" "text" NOT NULL,
    "operator" "text" NOT NULL,
    "threshold" real NOT NULL,
    "server_id" "text",
    "enabled" integer DEFAULT 1 NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: audit_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."audit_log" (
    "id" "text" NOT NULL,
    "timestamp" "text" NOT NULL,
    "actor" "text" NOT NULL,
    "action" "text" NOT NULL,
    "target_type" "text" NOT NULL,
    "target_id" "text",
    "detail_json" "text" DEFAULT '{}'::"text" NOT NULL,
    "ip" "text",
    "created_at" "text" NOT NULL
);

--
-- Name: credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."credentials" (
    "id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "type" "text" NOT NULL,
    "scope" "text" DEFAULT ''::"text" NOT NULL,
    "ciphertext" "bytea" NOT NULL,
    "masked_value" "text" NOT NULL,
    "last_used_at" "text",
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL,
    "username" "text" DEFAULT ''::"text" NOT NULL
);

--
-- Name: custom_nodes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."custom_nodes" (
    "id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "description" "text" DEFAULT ''::"text" NOT NULL,
    "node_type" "text" NOT NULL,
    "summary" "text" DEFAULT ''::"text" NOT NULL,
    "config_json" "text" DEFAULT '{}'::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: deploy_batch_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."deploy_batch_items" (
    "id" "text" NOT NULL,
    "batch_id" "text" NOT NULL,
    "item_index" integer NOT NULL,
    "artifact_id" "text" NOT NULL,
    "artifact_name" "text" NOT NULL,
    "server_id" "text" NOT NULL,
    "server_name" "text" NOT NULL,
    "config_json" "text" NOT NULL,
    "status" "text" NOT NULL,
    "message" "text" DEFAULT ''::"text" NOT NULL,
    "attempt" integer DEFAULT 0 NOT NULL,
    "started_at" "text",
    "finished_at" "text"
);

--
-- Name: deploy_batches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."deploy_batches" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "idempotency_key" "text" NOT NULL,
    "request_hash" "text" NOT NULL,
    "status" "text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: deploy_targets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."deploy_targets" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "server_id" "text" NOT NULL,
    "server_name" "text" NOT NULL,
    "status" "text" NOT NULL,
    "message" "text" DEFAULT ''::"text" NOT NULL,
    "started_at" "text" NOT NULL,
    "finished_at" "text"
);

--
-- Name: diagnosis_feedback; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."diagnosis_feedback" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "verdict" "text" NOT NULL,
    "correct_root_cause" "text" DEFAULT ''::"text" NOT NULL,
    "diagnosis_snapshot" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: dns_providers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."dns_providers" (
    "id" "text" NOT NULL,
    "type" "text" NOT NULL,
    "name" "text" NOT NULL,
    "credential_id" "text" NOT NULL,
    "base_domain" "text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: metric_samples; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."metric_samples" (
    "id" "text" NOT NULL,
    "server_id" "text" NOT NULL,
    "cpu_percent" real,
    "memory_percent" real,
    "disk_percent" real,
    "sampled_at" "text" NOT NULL
);

--
-- Name: notification_channels; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."notification_channels" (
    "id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "type" "text" NOT NULL,
    "enabled" integer DEFAULT 1 NOT NULL,
    "config_json" "text" DEFAULT '{}'::"text" NOT NULL,
    "secret_ciphertext" "bytea",
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: notification_routes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."notification_routes" (
    "id" "text" NOT NULL,
    "project_id" "text",
    "event" "text" NOT NULL,
    "channel_id" "text" NOT NULL,
    "enabled" integer DEFAULT 1 NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: notification_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."notification_templates" (
    "id" "text" NOT NULL,
    "project_id" "text",
    "event" "text" NOT NULL,
    "channel_id" "text",
    "title_template" "text" DEFAULT ''::"text" NOT NULL,
    "body_template" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: notify_config; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."notify_config" (
    "id" integer NOT NULL,
    "language" "text" DEFAULT 'zh-CN'::"text" NOT NULL,
    "updated_at" "text" DEFAULT ''::"text" NOT NULL,
    CONSTRAINT "notify_config_id_check" CHECK (("id" = 1))
);

--
-- Name: oauth_apps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."oauth_apps" (
    "provider" "text" NOT NULL,
    "client_id" "text" DEFAULT ''::"text" NOT NULL,
    "client_secret_ciphertext" "bytea",
    "base_url" "text" DEFAULT ''::"text" NOT NULL,
    "enabled" integer DEFAULT 0 NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: ops_chat_approvals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ops_chat_approvals" (
    "id" "text" NOT NULL,
    "session_id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "status" "text" NOT NULL,
    "instance_id" "text" NOT NULL,
    "expires_at" bigint NOT NULL,
    "body" "bytea" NOT NULL
);

--
-- Name: ops_chat_calls; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ops_chat_calls" (
    "id" "text" NOT NULL,
    "session_id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "server_id" "text" NOT NULL,
    "tool_id" "text" NOT NULL,
    "status" "text" NOT NULL,
    "ordinal" integer NOT NULL,
    "args_hash" "text" NOT NULL,
    "target_hash" "text" NOT NULL,
    "body" "bytea" NOT NULL,
    "created_at" bigint NOT NULL
);

--
-- Name: ops_chat_contexts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ops_chat_contexts" (
    "context_key" "text" NOT NULL,
    "session_id" "text" NOT NULL
);

--
-- Name: ops_chat_entries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ops_chat_entries" (
    "session_id" "text" NOT NULL,
    "seq" bigint NOT NULL,
    "kind" "text" NOT NULL,
    "run_id" "text" DEFAULT ''::"text" NOT NULL,
    "call_id" "text" DEFAULT ''::"text" NOT NULL,
    "context_allowed" integer DEFAULT 0 NOT NULL,
    "visible" integer DEFAULT 0 NOT NULL,
    "body" "bytea" NOT NULL,
    "body_size" integer NOT NULL,
    "created_at" bigint NOT NULL
);

--
-- Name: ops_chat_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ops_chat_runs" (
    "id" "text" NOT NULL,
    "session_id" "text" NOT NULL,
    "request_id" "text" NOT NULL,
    "payload_hash" "text" NOT NULL,
    "status" "text" NOT NULL,
    "cancel_requested" integer DEFAULT 0 NOT NULL,
    "body" "bytea" NOT NULL,
    "created_at" bigint NOT NULL
);

--
-- Name: ops_chat_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ops_chat_sessions" (
    "id" "text" NOT NULL,
    "revision" integer NOT NULL,
    "seq" integer DEFAULT 0 NOT NULL,
    "visible_count" integer DEFAULT 0 NOT NULL,
    "body_bytes" integer DEFAULT 0 NOT NULL,
    "active_run" "text" DEFAULT ''::"text" NOT NULL,
    "body" "bytea" NOT NULL,
    "created_at" bigint NOT NULL,
    "updated_at" bigint NOT NULL
);

--
-- Name: ops_chat_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."ops_chat_state" (
    "id" integer NOT NULL,
    "active_session" "text" DEFAULT ''::"text" NOT NULL,
    "lease_owner" "text" DEFAULT ''::"text" NOT NULL,
    "lease_until" bigint DEFAULT 0 NOT NULL,
    "keycheck" "bytea" NOT NULL
);

--
-- Name: pipeline_configs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."pipeline_configs" (
    "project_id" "text" NOT NULL,
    "spec_json" "text" DEFAULT '{}'::"text" NOT NULL,
    "spec_yaml" "text" DEFAULT ''::"text" NOT NULL,
    "status" "text" DEFAULT 'draft'::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL,
    "saved_at" "text"
);

--
-- Name: pipeline_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."pipeline_runs" (
    "id" "text" NOT NULL,
    "project_id" "text" NOT NULL,
    "status" "text" DEFAULT 'queued'::"text" NOT NULL,
    "trigger_type" "text" DEFAULT 'manual'::"text" NOT NULL,
    "trigger_branch" "text" DEFAULT ''::"text" NOT NULL,
    "trigger_commit" "text" DEFAULT ''::"text" NOT NULL,
    "trigger_actor" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "started_at" "text",
    "finished_at" "text",
    "resolved_environment" "text" DEFAULT ''::"text" NOT NULL,
    "resolved_target_server_ids" "text" DEFAULT '[]'::"text" NOT NULL,
    "failure_log" "text" DEFAULT ''::"text" NOT NULL,
    "diagnosis_json" "text" DEFAULT ''::"text" NOT NULL,
    "params_json" "text" DEFAULT '{}'::"text" NOT NULL,
    "chain_source_run_id" "text" DEFAULT ''::"text" NOT NULL,
    "chain_depth" integer DEFAULT 0 NOT NULL,
    "spec_source" "text" DEFAULT ''::"text" NOT NULL,
    "spec_source_ref" "text" DEFAULT ''::"text" NOT NULL,
    "spec_source_file" "text" DEFAULT ''::"text" NOT NULL,
    "spec_source_fallback" "text" DEFAULT ''::"text" NOT NULL,
    "execution_mode" "text" DEFAULT 'pending'::"text" NOT NULL
);

--
-- Name: pipeline_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."pipeline_settings" (
    "project_id" "text" NOT NULL,
    "build_json" "text" DEFAULT '{}'::"text" NOT NULL,
    "environments_json" "text" DEFAULT '[]'::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL,
    "steps_json" "text" DEFAULT '[]'::"text" NOT NULL
);

--
-- Name: pipeline_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."pipeline_templates" (
    "id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "description" "text" DEFAULT ''::"text" NOT NULL,
    "stages_json" "text" DEFAULT '[]'::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: pipeline_triggers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."pipeline_triggers" (
    "project_id" "text" NOT NULL,
    "webhook_token" "text" NOT NULL,
    "webhook_secret_ciphertext" "bytea" NOT NULL,
    "events_json" "text" DEFAULT '{}'::"text" NOT NULL,
    "branch_mappings_json" "text" DEFAULT '[]'::"text" NOT NULL,
    "unmatched_policy" "text" DEFAULT 'record'::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL,
    "path_filters_json" "text" DEFAULT '[]'::"text" NOT NULL
);

--
-- Name: project_chain_targets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."project_chain_targets" (
    "id" "text" NOT NULL,
    "project_id" "text" NOT NULL,
    "downstream_project_id" "text" NOT NULL,
    "branch" "text" DEFAULT ''::"text" NOT NULL,
    "enabled" integer DEFAULT 1 NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: project_concurrency; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."project_concurrency" (
    "project_id" "text" NOT NULL,
    "max_concurrent" integer DEFAULT 0 NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: project_crons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."project_crons" (
    "project_id" "text" NOT NULL,
    "expression" "text" DEFAULT ''::"text" NOT NULL,
    "branch" "text" DEFAULT ''::"text" NOT NULL,
    "enabled" integer DEFAULT 0 NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: project_parameters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."project_parameters" (
    "project_id" "text" NOT NULL,
    "defs_json" "text" DEFAULT '[]'::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: project_runners; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."project_runners" (
    "project_id" "text" NOT NULL,
    "runner_server_id" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: projects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."projects" (
    "id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "repo_url" "text" NOT NULL,
    "default_branch" "text" DEFAULT ''::"text" NOT NULL,
    "credential_id" "text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL,
    "pac_enabled" integer DEFAULT 0 NOT NULL,
    "pr_status_enabled" integer DEFAULT 0 NOT NULL
);

--
-- Name: proxy_routes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."proxy_routes" (
    "id" "text" NOT NULL,
    "server_id" "text" NOT NULL,
    "domain" "text" NOT NULL,
    "upstream_container" "text" NOT NULL,
    "upstream_port" integer NOT NULL,
    "tls_mode" "text" DEFAULT 'auto'::"text" NOT NULL,
    "enabled" integer DEFAULT 1 NOT NULL,
    "cert_status" "text" DEFAULT 'pending'::"text" NOT NULL,
    "cert_detail" "text",
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL,
    "config" "text" DEFAULT ''::"text" NOT NULL
);

--
-- Name: retention_config; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."retention_config" (
    "id" integer NOT NULL,
    "enabled" integer DEFAULT 0 NOT NULL,
    "keep_per_project" integer DEFAULT 0 NOT NULL,
    "max_age_days" integer DEFAULT 0 NOT NULL,
    "updated_at" "text" DEFAULT ''::"text" NOT NULL,
    CONSTRAINT "retention_config_id_check" CHECK (("id" = 1))
);

--
-- Name: role_permissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."role_permissions" (
    "role" character varying(64) NOT NULL,
    "permission" character varying(128) NOT NULL
);

--
-- Name: run_approvals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."run_approvals" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "stage_id" "text" NOT NULL,
    "stage_name" "text" DEFAULT ''::"text" NOT NULL,
    "status" "text" DEFAULT 'pending'::"text" NOT NULL,
    "decided_by" "text" DEFAULT ''::"text" NOT NULL,
    "decided_at" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: run_artifacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."run_artifacts" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "type" "text" NOT NULL,
    "name" "text" NOT NULL,
    "reference" "text" NOT NULL,
    "size_bytes" integer DEFAULT 0 NOT NULL,
    "metadata_json" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: run_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."run_logs" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "seq" integer NOT NULL,
    "ts" "text" NOT NULL,
    "stream" "text" NOT NULL,
    "step_ordinal" integer NOT NULL,
    "text" "text" NOT NULL
);

--
-- Name: run_promotions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."run_promotions" (
    "id" "text" NOT NULL,
    "project_id" "text" NOT NULL,
    "source_run_id" "text" NOT NULL,
    "from_environment" "text" DEFAULT ''::"text" NOT NULL,
    "target_environment" "text" NOT NULL,
    "status" "text" DEFAULT 'pending'::"text" NOT NULL,
    "approval_stage" "text" DEFAULT ''::"text" NOT NULL,
    "promoted_by" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "decided_at" "text" DEFAULT ''::"text" NOT NULL,
    "initiated_by" character varying(64) DEFAULT ''::character varying NOT NULL
);

--
-- Name: run_steps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."run_steps" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "status" "text" DEFAULT 'pending'::"text" NOT NULL,
    "ordinal" integer DEFAULT 0 NOT NULL,
    "started_at" "text",
    "finished_at" "text",
    "stage" "text" DEFAULT ''::"text" NOT NULL
);

--
-- Name: run_test_reports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."run_test_reports" (
    "id" "text" NOT NULL,
    "run_id" "text" NOT NULL,
    "stage_id" "text" DEFAULT ''::"text" NOT NULL,
    "stage_name" "text" DEFAULT ''::"text" NOT NULL,
    "format" "text" DEFAULT 'junit'::"text" NOT NULL,
    "total" integer DEFAULT 0 NOT NULL,
    "passed" integer DEFAULT 0 NOT NULL,
    "failed" integer DEFAULT 0 NOT NULL,
    "skipped" integer DEFAULT 0 NOT NULL,
    "duration_sec" real DEFAULT 0 NOT NULL,
    "coverage_pct" real DEFAULT '-1'::integer NOT NULL,
    "gate_enabled" integer DEFAULT 0 NOT NULL,
    "gate_passed" integer DEFAULT 1 NOT NULL,
    "gate_reason" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: server_proxy_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."server_proxy_settings" (
    "server_id" character varying(255) NOT NULL,
    "mode" character varying(32) DEFAULT 'managed'::character varying NOT NULL,
    "external_config_path" "text" DEFAULT ''::"text" NOT NULL,
    "external_fragment_dir" "text" DEFAULT ''::"text" NOT NULL,
    "external_reload_cmd" "text" DEFAULT ''::"text" NOT NULL,
    "updated_at" character varying(32) DEFAULT ''::character varying NOT NULL
);

--
-- Name: servers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."servers" (
    "id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "host" "text" NOT NULL,
    "port" integer DEFAULT 22 NOT NULL,
    "username" "text" NOT NULL,
    "credential_id" "text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."sessions" (
    "token" "text" NOT NULL,
    "csrf_token" "text" NOT NULL,
    "created_at" "text" NOT NULL,
    "expires_at" "text" NOT NULL,
    "last_seen_at" "text" NOT NULL,
    "username" character varying(255) DEFAULT ''::character varying NOT NULL
);

--
-- Name: system_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."system_settings" (
    "key" character varying(255) NOT NULL,
    "value" "text" NOT NULL,
    "updated_at" character varying(32) NOT NULL
);

--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."users" (
    "id" character varying(36) NOT NULL,
    "username" character varying(255) NOT NULL,
    "password_hash" character varying(255) NOT NULL,
    "role" character varying(64) DEFAULT 'admin'::character varying NOT NULL,
    "created_at" character varying(32) NOT NULL,
    "updated_at" character varying(32) NOT NULL
);

--
-- Name: variable_groups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."variable_groups" (
    "id" "text" NOT NULL,
    "name" "text" NOT NULL,
    "description" "text" DEFAULT ''::"text" NOT NULL,
    "vars_json" "text" DEFAULT '[]'::"text" NOT NULL,
    "created_at" "text" NOT NULL,
    "updated_at" "text" NOT NULL
);

--
-- Name: webhook_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE "public"."webhook_deliveries" (
    "id" "text" NOT NULL,
    "project_id" "text" NOT NULL,
    "delivery_id" "text" NOT NULL,
    "event" "text" DEFAULT ''::"text" NOT NULL,
    "branch" "text" DEFAULT ''::"text" NOT NULL,
    "commit_sha" "text" DEFAULT ''::"text" NOT NULL,
    "run_id" "text" DEFAULT ''::"text" NOT NULL,
    "outcome" "text" DEFAULT ''::"text" NOT NULL,
    "created_at" "text" NOT NULL
);

--
-- Name: ai_config ai_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ai_config"
    ADD CONSTRAINT "ai_config_pkey" PRIMARY KEY ("id");

--
-- Name: anomaly_alerts anomaly_alerts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."anomaly_alerts"
    ADD CONSTRAINT "anomaly_alerts_pkey" PRIMARY KEY ("id");

--
-- Name: anomaly_rules anomaly_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."anomaly_rules"
    ADD CONSTRAINT "anomaly_rules_pkey" PRIMARY KEY ("id");

--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."audit_log"
    ADD CONSTRAINT "audit_log_pkey" PRIMARY KEY ("id");

--
-- Name: credentials credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."credentials"
    ADD CONSTRAINT "credentials_pkey" PRIMARY KEY ("id");

--
-- Name: custom_nodes custom_nodes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."custom_nodes"
    ADD CONSTRAINT "custom_nodes_pkey" PRIMARY KEY ("id");

--
-- Name: deploy_batch_items deploy_batch_items_batch_id_item_index_server_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_batch_items"
    ADD CONSTRAINT "deploy_batch_items_batch_id_item_index_server_id_key" UNIQUE ("batch_id", "item_index", "server_id");

--
-- Name: deploy_batch_items deploy_batch_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_batch_items"
    ADD CONSTRAINT "deploy_batch_items_pkey" PRIMARY KEY ("id");

--
-- Name: deploy_batches deploy_batches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_batches"
    ADD CONSTRAINT "deploy_batches_pkey" PRIMARY KEY ("id");

--
-- Name: deploy_batches deploy_batches_run_id_idempotency_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_batches"
    ADD CONSTRAINT "deploy_batches_run_id_idempotency_key_key" UNIQUE ("run_id", "idempotency_key");

--
-- Name: deploy_targets deploy_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_targets"
    ADD CONSTRAINT "deploy_targets_pkey" PRIMARY KEY ("id");

--
-- Name: diagnosis_feedback diagnosis_feedback_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."diagnosis_feedback"
    ADD CONSTRAINT "diagnosis_feedback_pkey" PRIMARY KEY ("id");

--
-- Name: diagnosis_feedback diagnosis_feedback_run_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."diagnosis_feedback"
    ADD CONSTRAINT "diagnosis_feedback_run_id_key" UNIQUE ("run_id");

--
-- Name: dns_providers dns_providers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."dns_providers"
    ADD CONSTRAINT "dns_providers_pkey" PRIMARY KEY ("id");

--
-- Name: metric_samples metric_samples_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."metric_samples"
    ADD CONSTRAINT "metric_samples_pkey" PRIMARY KEY ("id");

--
-- Name: notification_channels notification_channels_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."notification_channels"
    ADD CONSTRAINT "notification_channels_pkey" PRIMARY KEY ("id");

--
-- Name: notification_routes notification_routes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."notification_routes"
    ADD CONSTRAINT "notification_routes_pkey" PRIMARY KEY ("id");

--
-- Name: notification_templates notification_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."notification_templates"
    ADD CONSTRAINT "notification_templates_pkey" PRIMARY KEY ("id");

--
-- Name: notify_config notify_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."notify_config"
    ADD CONSTRAINT "notify_config_pkey" PRIMARY KEY ("id");

--
-- Name: oauth_apps oauth_apps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."oauth_apps"
    ADD CONSTRAINT "oauth_apps_pkey" PRIMARY KEY ("provider");

--
-- Name: ops_chat_approvals ops_chat_approvals_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_approvals"
    ADD CONSTRAINT "ops_chat_approvals_pkey" PRIMARY KEY ("id");

--
-- Name: ops_chat_approvals ops_chat_approvals_run_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_approvals"
    ADD CONSTRAINT "ops_chat_approvals_run_id_key" UNIQUE ("run_id");

--
-- Name: ops_chat_calls ops_chat_calls_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_calls"
    ADD CONSTRAINT "ops_chat_calls_pkey" PRIMARY KEY ("id");

--
-- Name: ops_chat_contexts ops_chat_contexts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_contexts"
    ADD CONSTRAINT "ops_chat_contexts_pkey" PRIMARY KEY ("context_key");

--
-- Name: ops_chat_entries ops_chat_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_entries"
    ADD CONSTRAINT "ops_chat_entries_pkey" PRIMARY KEY ("session_id", "seq");

--
-- Name: ops_chat_runs ops_chat_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_runs"
    ADD CONSTRAINT "ops_chat_runs_pkey" PRIMARY KEY ("id");

--
-- Name: ops_chat_runs ops_chat_runs_request_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_runs"
    ADD CONSTRAINT "ops_chat_runs_request_id_key" UNIQUE ("request_id");

--
-- Name: ops_chat_sessions ops_chat_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_sessions"
    ADD CONSTRAINT "ops_chat_sessions_pkey" PRIMARY KEY ("id");

--
-- Name: ops_chat_state ops_chat_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_state"
    ADD CONSTRAINT "ops_chat_state_pkey" PRIMARY KEY ("id");

--
-- Name: pipeline_configs pipeline_configs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_configs"
    ADD CONSTRAINT "pipeline_configs_pkey" PRIMARY KEY ("project_id");

--
-- Name: pipeline_runs pipeline_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_runs"
    ADD CONSTRAINT "pipeline_runs_pkey" PRIMARY KEY ("id");

--
-- Name: pipeline_settings pipeline_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_settings"
    ADD CONSTRAINT "pipeline_settings_pkey" PRIMARY KEY ("project_id");

--
-- Name: pipeline_templates pipeline_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_templates"
    ADD CONSTRAINT "pipeline_templates_pkey" PRIMARY KEY ("id");

--
-- Name: pipeline_triggers pipeline_triggers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_triggers"
    ADD CONSTRAINT "pipeline_triggers_pkey" PRIMARY KEY ("project_id");

--
-- Name: pipeline_triggers pipeline_triggers_webhook_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_triggers"
    ADD CONSTRAINT "pipeline_triggers_webhook_token_key" UNIQUE ("webhook_token");

--
-- Name: project_chain_targets project_chain_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_chain_targets"
    ADD CONSTRAINT "project_chain_targets_pkey" PRIMARY KEY ("id");

--
-- Name: project_concurrency project_concurrency_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_concurrency"
    ADD CONSTRAINT "project_concurrency_pkey" PRIMARY KEY ("project_id");

--
-- Name: project_crons project_crons_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_crons"
    ADD CONSTRAINT "project_crons_pkey" PRIMARY KEY ("project_id");

--
-- Name: project_parameters project_parameters_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_parameters"
    ADD CONSTRAINT "project_parameters_pkey" PRIMARY KEY ("project_id");

--
-- Name: project_runners project_runners_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_runners"
    ADD CONSTRAINT "project_runners_pkey" PRIMARY KEY ("project_id");

--
-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."projects"
    ADD CONSTRAINT "projects_pkey" PRIMARY KEY ("id");

--
-- Name: proxy_routes proxy_routes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."proxy_routes"
    ADD CONSTRAINT "proxy_routes_pkey" PRIMARY KEY ("id");

--
-- Name: retention_config retention_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."retention_config"
    ADD CONSTRAINT "retention_config_pkey" PRIMARY KEY ("id");

--
-- Name: role_permissions role_permissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."role_permissions"
    ADD CONSTRAINT "role_permissions_pkey" PRIMARY KEY ("role", "permission");

--
-- Name: run_approvals run_approvals_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_approvals"
    ADD CONSTRAINT "run_approvals_pkey" PRIMARY KEY ("id");

--
-- Name: run_approvals run_approvals_run_id_stage_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_approvals"
    ADD CONSTRAINT "run_approvals_run_id_stage_id_key" UNIQUE ("run_id", "stage_id");

--
-- Name: run_artifacts run_artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_artifacts"
    ADD CONSTRAINT "run_artifacts_pkey" PRIMARY KEY ("id");

--
-- Name: run_logs run_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_logs"
    ADD CONSTRAINT "run_logs_pkey" PRIMARY KEY ("id");

--
-- Name: run_promotions run_promotions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_promotions"
    ADD CONSTRAINT "run_promotions_pkey" PRIMARY KEY ("id");

--
-- Name: run_steps run_steps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_steps"
    ADD CONSTRAINT "run_steps_pkey" PRIMARY KEY ("id");

--
-- Name: run_test_reports run_test_reports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_test_reports"
    ADD CONSTRAINT "run_test_reports_pkey" PRIMARY KEY ("id");

--
-- Name: server_proxy_settings server_proxy_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."server_proxy_settings"
    ADD CONSTRAINT "server_proxy_settings_pkey" PRIMARY KEY ("server_id");

--
-- Name: servers servers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."servers"
    ADD CONSTRAINT "servers_pkey" PRIMARY KEY ("id");

--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."sessions"
    ADD CONSTRAINT "sessions_pkey" PRIMARY KEY ("token");

--
-- Name: system_settings system_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."system_settings"
    ADD CONSTRAINT "system_settings_pkey" PRIMARY KEY ("key");

--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."users"
    ADD CONSTRAINT "users_pkey" PRIMARY KEY ("id");

--
-- Name: users users_username_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."users"
    ADD CONSTRAINT "users_username_key" UNIQUE ("username");

--
-- Name: variable_groups variable_groups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."variable_groups"
    ADD CONSTRAINT "variable_groups_pkey" PRIMARY KEY ("id");

--
-- Name: webhook_deliveries webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."webhook_deliveries"
    ADD CONSTRAINT "webhook_deliveries_pkey" PRIMARY KEY ("id");

--
-- Name: webhook_deliveries webhook_deliveries_project_id_delivery_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."webhook_deliveries"
    ADD CONSTRAINT "webhook_deliveries_project_id_delivery_id_key" UNIQUE ("project_id", "delivery_id");

--
-- Name: idx_anomaly_alerts_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_anomaly_alerts_created_at" ON "public"."anomaly_alerts" USING "btree" ("created_at");

--
-- Name: idx_anomaly_alerts_server_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_anomaly_alerts_server_id" ON "public"."anomaly_alerts" USING "btree" ("server_id");

--
-- Name: idx_audit_log_action; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_audit_log_action" ON "public"."audit_log" USING "btree" ("action");

--
-- Name: idx_audit_log_target_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_audit_log_target_type" ON "public"."audit_log" USING "btree" ("target_type");

--
-- Name: idx_audit_log_timestamp; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_audit_log_timestamp" ON "public"."audit_log" USING "btree" ("timestamp" DESC, "id" DESC);

--
-- Name: idx_custom_nodes_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "idx_custom_nodes_name" ON "public"."custom_nodes" USING "btree" ("name");

--
-- Name: idx_deploy_batch_items_batch; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_deploy_batch_items_batch" ON "public"."deploy_batch_items" USING "btree" ("batch_id", "item_index");

--
-- Name: idx_deploy_batches_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_deploy_batches_run" ON "public"."deploy_batches" USING "btree" ("run_id", "created_at");

--
-- Name: idx_deploy_targets_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_deploy_targets_run" ON "public"."deploy_targets" USING "btree" ("run_id");

--
-- Name: idx_diagnosis_feedback_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_diagnosis_feedback_created_at" ON "public"."diagnosis_feedback" USING "btree" ("created_at");

--
-- Name: idx_dns_providers_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_dns_providers_type" ON "public"."dns_providers" USING "btree" ("type");

--
-- Name: idx_metric_samples_server_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_metric_samples_server_time" ON "public"."metric_samples" USING "btree" ("server_id", "sampled_at");

--
-- Name: idx_metric_samples_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_metric_samples_time" ON "public"."metric_samples" USING "btree" ("sampled_at");

--
-- Name: idx_notification_routes_event; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_notification_routes_event" ON "public"."notification_routes" USING "btree" ("event");

--
-- Name: idx_notification_templates_event; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_notification_templates_event" ON "public"."notification_templates" USING "btree" ("event");

--
-- Name: idx_pipeline_runs_project_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_pipeline_runs_project_id" ON "public"."pipeline_runs" USING "btree" ("project_id");

--
-- Name: idx_pipeline_templates_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "idx_pipeline_templates_name" ON "public"."pipeline_templates" USING "btree" ("name");

--
-- Name: idx_project_chain_targets_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_project_chain_targets_project" ON "public"."project_chain_targets" USING "btree" ("project_id");

--
-- Name: idx_project_crons_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_project_crons_enabled" ON "public"."project_crons" USING "btree" ("enabled");

--
-- Name: idx_projects_credential_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_projects_credential_id" ON "public"."projects" USING "btree" ("credential_id");

--
-- Name: idx_proxy_routes_domain; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "idx_proxy_routes_domain" ON "public"."proxy_routes" USING "btree" ("domain");

--
-- Name: idx_proxy_routes_server; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_proxy_routes_server" ON "public"."proxy_routes" USING "btree" ("server_id");

--
-- Name: idx_run_approvals_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_run_approvals_run" ON "public"."run_approvals" USING "btree" ("run_id");

--
-- Name: idx_run_artifacts_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_run_artifacts_run" ON "public"."run_artifacts" USING "btree" ("run_id");

--
-- Name: idx_run_logs_run_seq; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_run_logs_run_seq" ON "public"."run_logs" USING "btree" ("run_id", "seq");

--
-- Name: idx_run_promotions_proj; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_run_promotions_proj" ON "public"."run_promotions" USING "btree" ("project_id");

--
-- Name: idx_run_promotions_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_run_promotions_run" ON "public"."run_promotions" USING "btree" ("source_run_id");

--
-- Name: idx_run_steps_run_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_run_steps_run_id" ON "public"."run_steps" USING "btree" ("run_id");

--
-- Name: idx_run_test_reports_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_run_test_reports_run" ON "public"."run_test_reports" USING "btree" ("run_id");

--
-- Name: idx_servers_credential_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_servers_credential_id" ON "public"."servers" USING "btree" ("credential_id");

--
-- Name: idx_variable_groups_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "idx_variable_groups_name" ON "public"."variable_groups" USING "btree" ("name");

--
-- Name: idx_webhook_deliveries_project_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "idx_webhook_deliveries_project_id" ON "public"."webhook_deliveries" USING "btree" ("project_id");

--
-- Name: ops_chat_calls_run; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "ops_chat_calls_run" ON "public"."ops_chat_calls" USING "btree" ("run_id", "ordinal");

--
-- Name: ops_chat_contexts_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "ops_chat_contexts_session" ON "public"."ops_chat_contexts" USING "btree" ("session_id");

--
-- Name: ops_chat_runs_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX "ops_chat_runs_session" ON "public"."ops_chat_runs" USING "btree" ("session_id", "created_at");

--
-- Name: uq_project_chain_targets; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX "uq_project_chain_targets" ON "public"."project_chain_targets" USING "btree" ("project_id", "downstream_project_id", "branch");

--
-- Name: audit_log audit_log_no_delete; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER "audit_log_no_delete" BEFORE DELETE ON "public"."audit_log" FOR EACH ROW EXECUTE FUNCTION "public"."audit_log_no_delete"();

--
-- Name: audit_log audit_log_no_update; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER "audit_log_no_update" BEFORE UPDATE ON "public"."audit_log" FOR EACH ROW EXECUTE FUNCTION "public"."audit_log_no_update"();

--
-- Name: deploy_batch_items deploy_batch_items_batch_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_batch_items"
    ADD CONSTRAINT "deploy_batch_items_batch_id_fkey" FOREIGN KEY ("batch_id") REFERENCES "public"."deploy_batches"("id") ON DELETE CASCADE;

--
-- Name: deploy_batches deploy_batches_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_batches"
    ADD CONSTRAINT "deploy_batches_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."pipeline_runs"("id") ON DELETE CASCADE;

--
-- Name: deploy_targets deploy_targets_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."deploy_targets"
    ADD CONSTRAINT "deploy_targets_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."pipeline_runs"("id");

--
-- Name: diagnosis_feedback diagnosis_feedback_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."diagnosis_feedback"
    ADD CONSTRAINT "diagnosis_feedback_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."pipeline_runs"("id") ON DELETE CASCADE;

--
-- Name: notification_routes notification_routes_channel_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."notification_routes"
    ADD CONSTRAINT "notification_routes_channel_id_fkey" FOREIGN KEY ("channel_id") REFERENCES "public"."notification_channels"("id") ON DELETE CASCADE;

--
-- Name: notification_templates notification_templates_channel_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."notification_templates"
    ADD CONSTRAINT "notification_templates_channel_id_fkey" FOREIGN KEY ("channel_id") REFERENCES "public"."notification_channels"("id") ON DELETE CASCADE;

--
-- Name: ops_chat_approvals ops_chat_approvals_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_approvals"
    ADD CONSTRAINT "ops_chat_approvals_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."ops_chat_runs"("id") ON DELETE CASCADE;

--
-- Name: ops_chat_approvals ops_chat_approvals_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_approvals"
    ADD CONSTRAINT "ops_chat_approvals_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "public"."ops_chat_sessions"("id") ON DELETE CASCADE;

--
-- Name: ops_chat_calls ops_chat_calls_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_calls"
    ADD CONSTRAINT "ops_chat_calls_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."ops_chat_runs"("id") ON DELETE CASCADE;

--
-- Name: ops_chat_calls ops_chat_calls_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_calls"
    ADD CONSTRAINT "ops_chat_calls_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "public"."ops_chat_sessions"("id") ON DELETE CASCADE;

--
-- Name: ops_chat_contexts ops_chat_contexts_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_contexts"
    ADD CONSTRAINT "ops_chat_contexts_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "public"."ops_chat_sessions"("id") ON DELETE CASCADE;

--
-- Name: ops_chat_entries ops_chat_entries_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_entries"
    ADD CONSTRAINT "ops_chat_entries_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "public"."ops_chat_sessions"("id") ON DELETE CASCADE;

--
-- Name: ops_chat_runs ops_chat_runs_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."ops_chat_runs"
    ADD CONSTRAINT "ops_chat_runs_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "public"."ops_chat_sessions"("id") ON DELETE CASCADE;

--
-- Name: pipeline_configs pipeline_configs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_configs"
    ADD CONSTRAINT "pipeline_configs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: pipeline_runs pipeline_runs_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_runs"
    ADD CONSTRAINT "pipeline_runs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: pipeline_settings pipeline_settings_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_settings"
    ADD CONSTRAINT "pipeline_settings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: pipeline_triggers pipeline_triggers_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."pipeline_triggers"
    ADD CONSTRAINT "pipeline_triggers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: project_chain_targets project_chain_targets_downstream_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_chain_targets"
    ADD CONSTRAINT "project_chain_targets_downstream_project_id_fkey" FOREIGN KEY ("downstream_project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: project_chain_targets project_chain_targets_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_chain_targets"
    ADD CONSTRAINT "project_chain_targets_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: project_concurrency project_concurrency_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_concurrency"
    ADD CONSTRAINT "project_concurrency_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: project_crons project_crons_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_crons"
    ADD CONSTRAINT "project_crons_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: project_parameters project_parameters_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_parameters"
    ADD CONSTRAINT "project_parameters_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: project_runners project_runners_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."project_runners"
    ADD CONSTRAINT "project_runners_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: projects projects_credential_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."projects"
    ADD CONSTRAINT "projects_credential_id_fkey" FOREIGN KEY ("credential_id") REFERENCES "public"."credentials"("id") ON DELETE RESTRICT;

--
-- Name: run_approvals run_approvals_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_approvals"
    ADD CONSTRAINT "run_approvals_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."pipeline_runs"("id") ON DELETE CASCADE;

--
-- Name: run_artifacts run_artifacts_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_artifacts"
    ADD CONSTRAINT "run_artifacts_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."pipeline_runs"("id") ON DELETE CASCADE;

--
-- Name: run_promotions run_promotions_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_promotions"
    ADD CONSTRAINT "run_promotions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- Name: run_promotions run_promotions_source_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_promotions"
    ADD CONSTRAINT "run_promotions_source_run_id_fkey" FOREIGN KEY ("source_run_id") REFERENCES "public"."pipeline_runs"("id") ON DELETE CASCADE;

--
-- Name: run_steps run_steps_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_steps"
    ADD CONSTRAINT "run_steps_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."pipeline_runs"("id") ON DELETE CASCADE;

--
-- Name: run_test_reports run_test_reports_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."run_test_reports"
    ADD CONSTRAINT "run_test_reports_run_id_fkey" FOREIGN KEY ("run_id") REFERENCES "public"."pipeline_runs"("id") ON DELETE CASCADE;

--
-- Name: servers servers_credential_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."servers"
    ADD CONSTRAINT "servers_credential_id_fkey" FOREIGN KEY ("credential_id") REFERENCES "public"."credentials"("id") ON DELETE RESTRICT;

--
-- Name: webhook_deliveries webhook_deliveries_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY "public"."webhook_deliveries"
    ADD CONSTRAINT "webhook_deliveries_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "public"."projects"("id") ON DELETE CASCADE;

--
-- PostgreSQL database dump complete
--

\unrestrict VQmJ6gvYd4ww9p1b0c9bF3eeU4pzRP0n5Aj7EhWc7mpdJIXZGZbXhPbtSjcVADa
