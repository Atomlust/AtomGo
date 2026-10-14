-- AtomGo MySQL schema baseline (v1.0.0). Single-file squash of all v1 migrations.
-- Greenfield: drop any pre-v1 database and let the migrator recreate from this file.
SET FOREIGN_KEY_CHECKS=0;
--
-- Host: localhost    Database: atomgo_squash
-- ------------------------------------------------------
-- Server version	8.4.11

--
-- Table structure for table `ai_config`
--

DROP TABLE IF EXISTS `ai_config`;
CREATE TABLE `ai_config` (
  `id` int NOT NULL,
  `provider` varchar(255) NOT NULL DEFAULT '',
  `base_url` varchar(1024) NOT NULL DEFAULT '',
  `model` varchar(255) NOT NULL DEFAULT '',
  `api_key_ciphertext` blob,
  `budget_json` longtext NOT NULL DEFAULT (_utf8mb4'{"monthlyTokenLimit":null}'),
  `enabled` tinyint NOT NULL DEFAULT '0',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  CONSTRAINT `ai_config_chk_1` CHECK ((`id` = 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `anomaly_alerts`
--

DROP TABLE IF EXISTS `anomaly_alerts`;
CREATE TABLE `anomaly_alerts` (
  `id` varchar(64) NOT NULL,
  `server_id` varchar(64) NOT NULL,
  `server_name` varchar(255) NOT NULL,
  `metric` varchar(255) NOT NULL,
  `operator` varchar(255) NOT NULL,
  `threshold` double NOT NULL,
  `value` double NOT NULL,
  `message` varchar(2048) NOT NULL,
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_anomaly_alerts_created_at` (`created_at`),
  KEY `idx_anomaly_alerts_server_id` (`server_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `anomaly_rules`
--

DROP TABLE IF EXISTS `anomaly_rules`;
CREATE TABLE `anomaly_rules` (
  `id` varchar(64) NOT NULL,
  `metric` varchar(255) NOT NULL,
  `operator` varchar(255) NOT NULL,
  `threshold` double NOT NULL,
  `server_id` varchar(64) DEFAULT NULL,
  `enabled` tinyint NOT NULL DEFAULT '1',
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `audit_log`
--

DROP TABLE IF EXISTS `audit_log`;
CREATE TABLE `audit_log` (
  `id` varchar(64) NOT NULL,
  `timestamp` varchar(32) NOT NULL,
  `actor` varchar(255) NOT NULL,
  `action` varchar(255) NOT NULL,
  `target_type` varchar(255) NOT NULL,
  `target_id` varchar(64) DEFAULT NULL,
  `detail_json` longtext NOT NULL DEFAULT (_utf8mb4'{}'),
  `ip` varchar(255) DEFAULT NULL,
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_audit_log_timestamp` (`timestamp` DESC,`id` DESC),
  KEY `idx_audit_log_action` (`action`),
  KEY `idx_audit_log_target_type` (`target_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `credentials`
--

DROP TABLE IF EXISTS `credentials`;
CREATE TABLE `credentials` (
  `id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `type` varchar(255) NOT NULL,
  `scope` varchar(255) NOT NULL DEFAULT '',
  `ciphertext` blob NOT NULL,
  `masked_value` varchar(255) NOT NULL,
  `last_used_at` varchar(32) DEFAULT NULL,
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  `username` varchar(255) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `custom_nodes`
--

DROP TABLE IF EXISTS `custom_nodes`;
CREATE TABLE `custom_nodes` (
  `id` varchar(64) NOT NULL,
  `name` varchar(191) NOT NULL,
  `description` varchar(2048) NOT NULL DEFAULT '',
  `node_type` varchar(255) NOT NULL,
  `summary` varchar(2048) NOT NULL DEFAULT '',
  `config_json` longtext NOT NULL DEFAULT (_utf8mb4'{}'),
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_custom_nodes_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `deploy_batch_items`
--

DROP TABLE IF EXISTS `deploy_batch_items`;
CREATE TABLE `deploy_batch_items` (
  `id` varchar(64) NOT NULL,
  `batch_id` varchar(64) NOT NULL,
  `item_index` int NOT NULL,
  `artifact_id` varchar(64) NOT NULL,
  `artifact_name` varchar(255) NOT NULL,
  `server_id` varchar(64) NOT NULL,
  `server_name` varchar(255) NOT NULL,
  `config_json` text NOT NULL,
  `status` varchar(32) NOT NULL,
  `message` varchar(2048) NOT NULL DEFAULT '',
  `attempt` int NOT NULL DEFAULT '0',
  `started_at` varchar(32) DEFAULT NULL,
  `finished_at` varchar(32) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_deploy_batch_item_target` (`batch_id`,`item_index`,`server_id`),
  KEY `idx_deploy_batch_items_batch` (`batch_id`,`item_index`),
  CONSTRAINT `deploy_batch_items_ibfk_1` FOREIGN KEY (`batch_id`) REFERENCES `deploy_batches` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `deploy_batches`
--

DROP TABLE IF EXISTS `deploy_batches`;
CREATE TABLE `deploy_batches` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `idempotency_key` varchar(128) NOT NULL,
  `request_hash` char(64) NOT NULL,
  `status` varchar(32) NOT NULL,
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_deploy_batches_run_key` (`run_id`,`idempotency_key`),
  KEY `idx_deploy_batches_run` (`run_id`,`created_at`),
  CONSTRAINT `deploy_batches_ibfk_1` FOREIGN KEY (`run_id`) REFERENCES `pipeline_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `deploy_targets`
--

DROP TABLE IF EXISTS `deploy_targets`;
CREATE TABLE `deploy_targets` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `server_id` varchar(64) NOT NULL,
  `server_name` varchar(255) NOT NULL,
  `status` varchar(255) NOT NULL,
  `message` varchar(2048) NOT NULL DEFAULT '',
  `started_at` varchar(32) NOT NULL,
  `finished_at` varchar(32) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_deploy_targets_run` (`run_id`),
  CONSTRAINT `deploy_targets_ibfk_1` FOREIGN KEY (`run_id`) REFERENCES `pipeline_runs` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `diagnosis_feedback`
--

DROP TABLE IF EXISTS `diagnosis_feedback`;
CREATE TABLE `diagnosis_feedback` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `verdict` varchar(255) NOT NULL,
  `correct_root_cause` longtext NOT NULL DEFAULT (_utf8mb4''),
  `diagnosis_snapshot` longtext NOT NULL DEFAULT (_utf8mb4''),
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `run_id` (`run_id`),
  KEY `idx_diagnosis_feedback_created_at` (`created_at`),
  CONSTRAINT `diagnosis_feedback_ibfk_1` FOREIGN KEY (`run_id`) REFERENCES `pipeline_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `dns_providers`
--

DROP TABLE IF EXISTS `dns_providers`;
CREATE TABLE `dns_providers` (
  `id` varchar(64) NOT NULL,
  `type` varchar(32) NOT NULL,
  `name` varchar(255) NOT NULL,
  `credential_id` varchar(64) NOT NULL,
  `base_domain` varchar(253) NOT NULL,
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_dns_providers_type` (`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `metric_samples`
--

DROP TABLE IF EXISTS `metric_samples`;
CREATE TABLE `metric_samples` (
  `id` varchar(64) NOT NULL,
  `server_id` varchar(64) NOT NULL,
  `cpu_percent` double DEFAULT NULL,
  `memory_percent` double DEFAULT NULL,
  `disk_percent` double DEFAULT NULL,
  `sampled_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_metric_samples_server_time` (`server_id`,`sampled_at`),
  KEY `idx_metric_samples_time` (`sampled_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `notification_channels`
--

DROP TABLE IF EXISTS `notification_channels`;
CREATE TABLE `notification_channels` (
  `id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `type` varchar(255) NOT NULL,
  `enabled` tinyint NOT NULL DEFAULT '1',
  `config_json` longtext NOT NULL DEFAULT (_utf8mb4'{}'),
  `secret_ciphertext` blob,
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `notification_routes`
--

DROP TABLE IF EXISTS `notification_routes`;
CREATE TABLE `notification_routes` (
  `id` varchar(64) NOT NULL,
  `project_id` varchar(64) DEFAULT NULL,
  `event` varchar(255) NOT NULL,
  `channel_id` varchar(64) NOT NULL,
  `enabled` tinyint NOT NULL DEFAULT '1',
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `channel_id` (`channel_id`),
  KEY `idx_notification_routes_event` (`event`),
  CONSTRAINT `notification_routes_ibfk_1` FOREIGN KEY (`channel_id`) REFERENCES `notification_channels` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `notification_templates`
--

DROP TABLE IF EXISTS `notification_templates`;
CREATE TABLE `notification_templates` (
  `id` varchar(64) NOT NULL,
  `project_id` varchar(64) DEFAULT NULL,
  `event` varchar(255) NOT NULL,
  `channel_id` varchar(64) DEFAULT NULL,
  `title_template` longtext NOT NULL DEFAULT (_utf8mb4''),
  `body_template` longtext NOT NULL DEFAULT (_utf8mb4''),
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `channel_id` (`channel_id`),
  KEY `idx_notification_templates_event` (`event`),
  CONSTRAINT `notification_templates_ibfk_1` FOREIGN KEY (`channel_id`) REFERENCES `notification_channels` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `notify_config`
--

DROP TABLE IF EXISTS `notify_config`;
CREATE TABLE `notify_config` (
  `id` int NOT NULL,
  `language` varchar(16) NOT NULL DEFAULT 'zh-CN',
  `updated_at` varchar(32) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  CONSTRAINT `notify_config_chk_1` CHECK ((`id` = 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `oauth_apps`
--

DROP TABLE IF EXISTS `oauth_apps`;
CREATE TABLE `oauth_apps` (
  `provider` varchar(64) NOT NULL,
  `client_id` varchar(255) NOT NULL DEFAULT '',
  `client_secret_ciphertext` blob,
  `base_url` varchar(1024) NOT NULL DEFAULT '',
  `enabled` tinyint NOT NULL DEFAULT '0',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`provider`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `ops_chat_approvals`
--

DROP TABLE IF EXISTS `ops_chat_approvals`;
CREATE TABLE `ops_chat_approvals` (
  `id` varchar(36) NOT NULL,
  `session_id` varchar(36) NOT NULL,
  `run_id` varchar(36) NOT NULL,
  `status` varchar(40) NOT NULL,
  `instance_id` varchar(36) NOT NULL,
  `expires_at` bigint NOT NULL,
  `body` mediumblob NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `run_id` (`run_id`),
  KEY `session_id` (`session_id`),
  CONSTRAINT `ops_chat_approvals_ibfk_1` FOREIGN KEY (`session_id`) REFERENCES `ops_chat_sessions` (`id`) ON DELETE CASCADE,
  CONSTRAINT `ops_chat_approvals_ibfk_2` FOREIGN KEY (`run_id`) REFERENCES `ops_chat_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `ops_chat_calls`
--

DROP TABLE IF EXISTS `ops_chat_calls`;
CREATE TABLE `ops_chat_calls` (
  `id` varchar(36) NOT NULL,
  `session_id` varchar(36) NOT NULL,
  `run_id` varchar(36) NOT NULL,
  `server_id` varchar(36) NOT NULL,
  `tool_id` varchar(40) NOT NULL,
  `status` varchar(40) NOT NULL,
  `ordinal` int NOT NULL,
  `args_hash` varchar(64) NOT NULL,
  `target_hash` varchar(64) NOT NULL,
  `body` mediumblob NOT NULL,
  `created_at` bigint NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `ops_chat_calls_run` (`run_id`,`ordinal`),
  KEY `session_id` (`session_id`),
  CONSTRAINT `ops_chat_calls_ibfk_1` FOREIGN KEY (`session_id`) REFERENCES `ops_chat_sessions` (`id`) ON DELETE CASCADE,
  CONSTRAINT `ops_chat_calls_ibfk_2` FOREIGN KEY (`run_id`) REFERENCES `ops_chat_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `ops_chat_contexts`
--

DROP TABLE IF EXISTS `ops_chat_contexts`;
CREATE TABLE `ops_chat_contexts` (
  `context_key` varchar(80) NOT NULL,
  `session_id` varchar(36) NOT NULL,
  PRIMARY KEY (`context_key`),
  KEY `ops_chat_contexts_session` (`session_id`),
  CONSTRAINT `ops_chat_contexts_ibfk_1` FOREIGN KEY (`session_id`) REFERENCES `ops_chat_sessions` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `ops_chat_entries`
--

DROP TABLE IF EXISTS `ops_chat_entries`;
CREATE TABLE `ops_chat_entries` (
  `session_id` varchar(36) NOT NULL,
  `seq` bigint NOT NULL,
  `kind` varchar(40) NOT NULL,
  `run_id` varchar(36) NOT NULL DEFAULT '',
  `call_id` varchar(36) NOT NULL DEFAULT '',
  `context_allowed` int NOT NULL DEFAULT '0',
  `visible` int NOT NULL DEFAULT '0',
  `body` mediumblob NOT NULL,
  `body_size` int NOT NULL,
  `created_at` bigint NOT NULL,
  PRIMARY KEY (`session_id`,`seq`),
  CONSTRAINT `ops_chat_entries_ibfk_1` FOREIGN KEY (`session_id`) REFERENCES `ops_chat_sessions` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `ops_chat_runs`
--

DROP TABLE IF EXISTS `ops_chat_runs`;
CREATE TABLE `ops_chat_runs` (
  `id` varchar(36) NOT NULL,
  `session_id` varchar(36) NOT NULL,
  `request_id` varchar(36) NOT NULL,
  `payload_hash` varchar(64) NOT NULL,
  `status` varchar(40) NOT NULL,
  `cancel_requested` int NOT NULL DEFAULT '0',
  `body` mediumblob NOT NULL,
  `created_at` bigint NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `request_id` (`request_id`),
  KEY `ops_chat_runs_session` (`session_id`,`created_at`),
  CONSTRAINT `ops_chat_runs_ibfk_1` FOREIGN KEY (`session_id`) REFERENCES `ops_chat_sessions` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `ops_chat_sessions`
--

DROP TABLE IF EXISTS `ops_chat_sessions`;
CREATE TABLE `ops_chat_sessions` (
  `id` varchar(36) NOT NULL,
  `revision` int NOT NULL,
  `seq` int NOT NULL DEFAULT '0',
  `visible_count` int NOT NULL DEFAULT '0',
  `body_bytes` int NOT NULL DEFAULT '0',
  `active_run` varchar(36) NOT NULL DEFAULT '',
  `body` mediumblob NOT NULL,
  `created_at` bigint NOT NULL,
  `updated_at` bigint NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `ops_chat_state`
--

DROP TABLE IF EXISTS `ops_chat_state`;
CREATE TABLE `ops_chat_state` (
  `id` int NOT NULL,
  `active_session` varchar(36) NOT NULL DEFAULT '',
  `lease_owner` varchar(36) NOT NULL DEFAULT '',
  `lease_until` bigint NOT NULL DEFAULT '0',
  `keycheck` blob NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `pipeline_configs`
--

DROP TABLE IF EXISTS `pipeline_configs`;
CREATE TABLE `pipeline_configs` (
  `project_id` varchar(64) NOT NULL,
  `spec_json` longtext NOT NULL DEFAULT (_utf8mb4'{}'),
  `spec_yaml` longtext NOT NULL DEFAULT (_utf8mb4''),
  `status` varchar(255) NOT NULL DEFAULT 'draft',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  `saved_at` varchar(40) DEFAULT NULL,
  PRIMARY KEY (`project_id`),
  CONSTRAINT `pipeline_configs_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `pipeline_runs`
--

DROP TABLE IF EXISTS `pipeline_runs`;
CREATE TABLE `pipeline_runs` (
  `id` varchar(64) NOT NULL,
  `project_id` varchar(64) NOT NULL,
  `status` varchar(255) NOT NULL DEFAULT 'queued',
  `trigger_type` varchar(255) NOT NULL DEFAULT 'manual',
  `trigger_branch` varchar(255) NOT NULL DEFAULT '',
  `trigger_commit` varchar(255) NOT NULL DEFAULT '',
  `trigger_actor` varchar(255) NOT NULL DEFAULT '',
  `created_at` varchar(32) NOT NULL,
  `started_at` varchar(32) DEFAULT NULL,
  `finished_at` varchar(32) DEFAULT NULL,
  `resolved_environment` varchar(255) NOT NULL DEFAULT '',
  `resolved_target_server_ids` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  `failure_log` longtext NOT NULL DEFAULT (_utf8mb4''),
  `diagnosis_json` longtext NOT NULL DEFAULT (_utf8mb4''),
  `params_json` longtext NOT NULL DEFAULT (_utf8mb4'{}'),
  `chain_source_run_id` varchar(64) NOT NULL DEFAULT '',
  `chain_depth` bigint NOT NULL DEFAULT '0',
  `spec_source` varchar(32) NOT NULL DEFAULT '',
  `spec_source_ref` varchar(255) NOT NULL DEFAULT '',
  `spec_source_file` varchar(255) NOT NULL DEFAULT '',
  `spec_source_fallback` varchar(64) NOT NULL DEFAULT '',
  `execution_mode` varchar(32) NOT NULL DEFAULT 'pending',
  PRIMARY KEY (`id`),
  KEY `idx_pipeline_runs_project_id` (`project_id`),
  CONSTRAINT `pipeline_runs_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `pipeline_settings`
--

DROP TABLE IF EXISTS `pipeline_settings`;
CREATE TABLE `pipeline_settings` (
  `project_id` varchar(64) NOT NULL,
  `build_json` longtext NOT NULL DEFAULT (_utf8mb4'{}'),
  `environments_json` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  `steps_json` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  PRIMARY KEY (`project_id`),
  CONSTRAINT `pipeline_settings_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `pipeline_templates`
--

DROP TABLE IF EXISTS `pipeline_templates`;
CREATE TABLE `pipeline_templates` (
  `id` varchar(64) NOT NULL,
  `name` varchar(191) NOT NULL,
  `description` varchar(2048) NOT NULL DEFAULT '',
  `stages_json` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_pipeline_templates_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `pipeline_triggers`
--

DROP TABLE IF EXISTS `pipeline_triggers`;
CREATE TABLE `pipeline_triggers` (
  `project_id` varchar(64) NOT NULL,
  `webhook_token` varchar(191) NOT NULL,
  `webhook_secret_ciphertext` blob NOT NULL,
  `events_json` longtext NOT NULL DEFAULT (_utf8mb4'{}'),
  `branch_mappings_json` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  `unmatched_policy` varchar(255) NOT NULL DEFAULT 'record',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  `path_filters_json` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  PRIMARY KEY (`project_id`),
  UNIQUE KEY `webhook_token` (`webhook_token`),
  CONSTRAINT `pipeline_triggers_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `project_chain_targets`
--

DROP TABLE IF EXISTS `project_chain_targets`;
CREATE TABLE `project_chain_targets` (
  `id` varchar(64) NOT NULL,
  `project_id` varchar(64) NOT NULL,
  `downstream_project_id` varchar(64) NOT NULL,
  `branch` varchar(255) NOT NULL DEFAULT '',
  `enabled` tinyint NOT NULL DEFAULT '1',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_project_chain_targets` (`project_id`,`downstream_project_id`,`branch`),
  KEY `downstream_project_id` (`downstream_project_id`),
  KEY `idx_project_chain_targets_project` (`project_id`),
  CONSTRAINT `project_chain_targets_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE,
  CONSTRAINT `project_chain_targets_ibfk_2` FOREIGN KEY (`downstream_project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `project_concurrency`
--

DROP TABLE IF EXISTS `project_concurrency`;
CREATE TABLE `project_concurrency` (
  `project_id` varchar(64) NOT NULL,
  `max_concurrent` bigint NOT NULL DEFAULT '0',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`project_id`),
  CONSTRAINT `project_concurrency_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `project_crons`
--

DROP TABLE IF EXISTS `project_crons`;
CREATE TABLE `project_crons` (
  `project_id` varchar(64) NOT NULL,
  `expression` varchar(255) NOT NULL DEFAULT '',
  `branch` varchar(255) NOT NULL DEFAULT '',
  `enabled` tinyint NOT NULL DEFAULT '0',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`project_id`),
  KEY `idx_project_crons_enabled` (`enabled`),
  CONSTRAINT `project_crons_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `project_parameters`
--

DROP TABLE IF EXISTS `project_parameters`;
CREATE TABLE `project_parameters` (
  `project_id` varchar(64) NOT NULL,
  `defs_json` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`project_id`),
  CONSTRAINT `project_parameters_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `project_runners`
--

DROP TABLE IF EXISTS `project_runners`;
CREATE TABLE `project_runners` (
  `project_id` varchar(64) NOT NULL,
  `runner_server_id` varchar(64) NOT NULL DEFAULT '',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`project_id`),
  CONSTRAINT `project_runners_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `projects`
--

DROP TABLE IF EXISTS `projects`;
CREATE TABLE `projects` (
  `id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `repo_url` varchar(1024) NOT NULL,
  `default_branch` varchar(255) NOT NULL DEFAULT '',
  `credential_id` varchar(64) NOT NULL,
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  `pac_enabled` tinyint NOT NULL DEFAULT '0',
  `pr_status_enabled` tinyint NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `idx_projects_credential_id` (`credential_id`),
  CONSTRAINT `projects_ibfk_1` FOREIGN KEY (`credential_id`) REFERENCES `credentials` (`id`) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `proxy_routes`
--

DROP TABLE IF EXISTS `proxy_routes`;
CREATE TABLE `proxy_routes` (
  `id` varchar(64) NOT NULL,
  `server_id` varchar(64) NOT NULL,
  `domain` varchar(253) NOT NULL,
  `upstream_container` varchar(255) NOT NULL,
  `upstream_port` int NOT NULL,
  `tls_mode` varchar(16) NOT NULL DEFAULT 'auto',
  `enabled` tinyint NOT NULL DEFAULT '1',
  `cert_status` varchar(16) NOT NULL DEFAULT 'pending',
  `cert_detail` text,
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  `config` longtext NOT NULL DEFAULT (_utf8mb4''),
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_proxy_routes_domain` (`domain`),
  KEY `idx_proxy_routes_server` (`server_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `retention_config`
--

DROP TABLE IF EXISTS `retention_config`;
CREATE TABLE `retention_config` (
  `id` int NOT NULL,
  `enabled` tinyint NOT NULL DEFAULT '0',
  `keep_per_project` int NOT NULL DEFAULT '0',
  `max_age_days` int NOT NULL DEFAULT '0',
  `updated_at` varchar(32) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  CONSTRAINT `retention_config_chk_1` CHECK ((`id` = 1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `role_permissions`
--

DROP TABLE IF EXISTS `role_permissions`;
CREATE TABLE `role_permissions` (
  `role` varchar(64) NOT NULL,
  `permission` varchar(128) NOT NULL,
  PRIMARY KEY (`role`,`permission`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `run_approvals`
--

DROP TABLE IF EXISTS `run_approvals`;
CREATE TABLE `run_approvals` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `stage_id` varchar(64) NOT NULL,
  `stage_name` varchar(255) NOT NULL DEFAULT '',
  `status` varchar(255) NOT NULL DEFAULT 'pending',
  `decided_by` varchar(255) NOT NULL DEFAULT '',
  `decided_at` varchar(32) NOT NULL DEFAULT '',
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `run_id` (`run_id`,`stage_id`),
  KEY `idx_run_approvals_run` (`run_id`),
  CONSTRAINT `run_approvals_ibfk_1` FOREIGN KEY (`run_id`) REFERENCES `pipeline_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `run_artifacts`
--

DROP TABLE IF EXISTS `run_artifacts`;
CREATE TABLE `run_artifacts` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `type` varchar(255) NOT NULL,
  `name` varchar(255) NOT NULL,
  `reference` varchar(2048) NOT NULL,
  `size_bytes` bigint NOT NULL DEFAULT '0',
  `metadata_json` longtext NOT NULL DEFAULT (_utf8mb4''),
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_run_artifacts_run` (`run_id`),
  CONSTRAINT `run_artifacts_ibfk_1` FOREIGN KEY (`run_id`) REFERENCES `pipeline_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `run_logs`
--

DROP TABLE IF EXISTS `run_logs`;
CREATE TABLE `run_logs` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `seq` bigint NOT NULL,
  `ts` varchar(32) NOT NULL,
  `stream` varchar(255) NOT NULL,
  `step_ordinal` bigint NOT NULL,
  `text` longtext NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_run_logs_run_seq` (`run_id`,`seq`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `run_promotions`
--

DROP TABLE IF EXISTS `run_promotions`;
CREATE TABLE `run_promotions` (
  `id` varchar(64) NOT NULL,
  `project_id` varchar(64) NOT NULL,
  `source_run_id` varchar(64) NOT NULL,
  `from_environment` varchar(255) NOT NULL DEFAULT '',
  `target_environment` varchar(255) NOT NULL,
  `status` varchar(255) NOT NULL DEFAULT 'pending',
  `approval_stage` varchar(255) NOT NULL DEFAULT '',
  `promoted_by` varchar(255) NOT NULL DEFAULT '',
  `created_at` varchar(32) NOT NULL,
  `decided_at` varchar(32) NOT NULL DEFAULT '',
  `initiated_by` varchar(64) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `idx_run_promotions_proj` (`project_id`),
  KEY `idx_run_promotions_run` (`source_run_id`),
  CONSTRAINT `run_promotions_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE,
  CONSTRAINT `run_promotions_ibfk_2` FOREIGN KEY (`source_run_id`) REFERENCES `pipeline_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `run_steps`
--

DROP TABLE IF EXISTS `run_steps`;
CREATE TABLE `run_steps` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `status` varchar(255) NOT NULL DEFAULT 'pending',
  `ordinal` bigint NOT NULL DEFAULT '0',
  `started_at` varchar(32) DEFAULT NULL,
  `finished_at` varchar(32) DEFAULT NULL,
  `stage` varchar(255) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `idx_run_steps_run_id` (`run_id`),
  CONSTRAINT `run_steps_ibfk_1` FOREIGN KEY (`run_id`) REFERENCES `pipeline_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `run_test_reports`
--

DROP TABLE IF EXISTS `run_test_reports`;
CREATE TABLE `run_test_reports` (
  `id` varchar(64) NOT NULL,
  `run_id` varchar(64) NOT NULL,
  `stage_id` varchar(64) NOT NULL DEFAULT '',
  `stage_name` varchar(255) NOT NULL DEFAULT '',
  `format` varchar(255) NOT NULL DEFAULT 'junit',
  `total` bigint NOT NULL DEFAULT '0',
  `passed` bigint NOT NULL DEFAULT '0',
  `failed` bigint NOT NULL DEFAULT '0',
  `skipped` bigint NOT NULL DEFAULT '0',
  `duration_sec` double NOT NULL DEFAULT '0',
  `coverage_pct` double NOT NULL DEFAULT '-1',
  `gate_enabled` tinyint NOT NULL DEFAULT '0',
  `gate_passed` tinyint NOT NULL DEFAULT '1',
  `gate_reason` varchar(2048) NOT NULL DEFAULT '',
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_run_test_reports_run` (`run_id`),
  CONSTRAINT `run_test_reports_ibfk_1` FOREIGN KEY (`run_id`) REFERENCES `pipeline_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--

--
-- Table structure for table `server_proxy_settings`
--

DROP TABLE IF EXISTS `server_proxy_settings`;
CREATE TABLE `server_proxy_settings` (
  `server_id` varchar(255) NOT NULL,
  `mode` varchar(32) NOT NULL DEFAULT 'managed',
  `external_config_path` text NOT NULL,
  `external_fragment_dir` text NOT NULL,
  `external_reload_cmd` text NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`server_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `servers`
--

DROP TABLE IF EXISTS `servers`;
CREATE TABLE `servers` (
  `id` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `host` varchar(255) NOT NULL,
  `port` bigint NOT NULL DEFAULT '22',
  `username` varchar(255) NOT NULL,
  `credential_id` varchar(64) NOT NULL,
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_servers_credential_id` (`credential_id`),
  CONSTRAINT `servers_ibfk_1` FOREIGN KEY (`credential_id`) REFERENCES `credentials` (`id`) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `sessions`
--

DROP TABLE IF EXISTS `sessions`;
CREATE TABLE `sessions` (
  `token` varchar(191) NOT NULL,
  `csrf_token` varchar(191) NOT NULL,
  `created_at` varchar(32) NOT NULL,
  `expires_at` varchar(32) NOT NULL,
  `last_seen_at` varchar(32) NOT NULL,
  `username` varchar(255) NOT NULL DEFAULT '',
  PRIMARY KEY (`token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `system_settings`
--

DROP TABLE IF EXISTS `system_settings`;
CREATE TABLE `system_settings` (
  `key` varchar(255) NOT NULL,
  `value` longtext NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `users`
--

DROP TABLE IF EXISTS `users`;
CREATE TABLE `users` (
  `id` varchar(36) NOT NULL,
  `username` varchar(255) NOT NULL,
  `password_hash` varchar(255) NOT NULL,
  `role` varchar(64) NOT NULL DEFAULT 'admin',
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `variable_groups`
--

DROP TABLE IF EXISTS `variable_groups`;
CREATE TABLE `variable_groups` (
  `id` varchar(64) NOT NULL,
  `name` varchar(191) NOT NULL,
  `description` varchar(2048) NOT NULL DEFAULT '',
  `vars_json` longtext NOT NULL DEFAULT (_utf8mb4'[]'),
  `created_at` varchar(32) NOT NULL,
  `updated_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_variable_groups_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

--
-- Table structure for table `webhook_deliveries`
--

DROP TABLE IF EXISTS `webhook_deliveries`;
CREATE TABLE `webhook_deliveries` (
  `id` varchar(64) NOT NULL,
  `project_id` varchar(64) NOT NULL,
  `delivery_id` varchar(191) NOT NULL,
  `event` varchar(255) NOT NULL DEFAULT '',
  `branch` varchar(255) NOT NULL DEFAULT '',
  `commit_sha` varchar(255) NOT NULL DEFAULT '',
  `run_id` varchar(64) NOT NULL DEFAULT '',
  `outcome` varchar(255) NOT NULL DEFAULT '',
  `created_at` varchar(32) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `project_id` (`project_id`,`delivery_id`),
  KEY `idx_webhook_deliveries_project_id` (`project_id`),
  CONSTRAINT `webhook_deliveries_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Dump completed on 2026-10-14  0:20:56
-- append-only 审计硬拦(与 0009_audit 同源):UPDATE/DELETE 直接报错回滚。
DROP TRIGGER IF EXISTS audit_log_no_update;
CREATE TRIGGER audit_log_no_update
BEFORE UPDATE ON audit_log FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_log is append-only: UPDATE forbidden';

DROP TRIGGER IF EXISTS audit_log_no_delete;
CREATE TRIGGER audit_log_no_delete
BEFORE DELETE ON audit_log FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_log is append-only: DELETE forbidden';

SET FOREIGN_KEY_CHECKS=1;
