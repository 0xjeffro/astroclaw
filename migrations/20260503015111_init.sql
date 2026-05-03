-- Create "app_agents_profiles" table
CREATE TABLE "public"."app_agents_profiles" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  "soul" text NOT NULL DEFAULT '',
  "model" text NOT NULL DEFAULT 'claude-sonnet-4-20250514',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create "app_chat_messages" table
CREATE TABLE "public"."app_chat_messages" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "session_id" uuid NOT NULL,
  "role" text NOT NULL,
  "content" text NOT NULL DEFAULT '',
  "tool_calls" text NULL,
  "tool_call_id" text NOT NULL DEFAULT '',
  "sequence_number" integer NOT NULL,
  "forwarded_from" uuid NULL,
  "reply_to" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "idx_app_chat_messages_session_seq" to table: "app_chat_messages"
CREATE INDEX "idx_app_chat_messages_session_seq" ON "public"."app_chat_messages" ("session_id", "sequence_number");
-- Create "app_chat_session_agents" table
CREATE TABLE "public"."app_chat_session_agents" (
  "session_id" uuid NOT NULL,
  "agent_id" uuid NOT NULL,
  "role" text NOT NULL DEFAULT 'primary',
  "joined_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("session_id", "agent_id")
);
-- Create "app_chat_session_members" table
CREATE TABLE "public"."app_chat_session_members" (
  "session_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "role" text NOT NULL DEFAULT 'guest',
  "joined_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("session_id", "user_id")
);
-- Create "app_chat_sessions" table
CREATE TABLE "public"."app_chat_sessions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "title" text NOT NULL DEFAULT '',
  "model" text NOT NULL DEFAULT '',
  "system_prompt" text NOT NULL DEFAULT '',
  "context_window" integer NOT NULL DEFAULT 0,
  "context_messages" text NOT NULL DEFAULT '[]',
  "context_summary" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create "app_chat_users" table
CREATE TABLE "public"."app_chat_users" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "email" text NOT NULL,
  "name" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "app_chat_users_email_key" UNIQUE ("email")
);
-- Create "app_notes_memories" table
CREATE TABLE "public"."app_notes_memories" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "agent_id" uuid NOT NULL,
  "content" text NOT NULL,
  "session_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create "app_notes_memory_sources" table
CREATE TABLE "public"."app_notes_memory_sources" (
  "memory_id" uuid NOT NULL,
  "message_id" uuid NOT NULL,
  PRIMARY KEY ("memory_id", "message_id")
);
-- Create "app_passwords_credentials" table
CREATE TABLE "public"."app_passwords_credentials" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  "value" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create "app_settings_prompt" table
CREATE TABLE "public"."app_settings_prompt" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  "value" text NOT NULL DEFAULT '',
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "app_settings_prompt_name_key" UNIQUE ("name")
);
