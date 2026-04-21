-- Create "messages" table
CREATE TABLE "public"."messages" ("id" uuid NOT NULL DEFAULT gen_random_uuid(), "session_id" uuid NOT NULL, "role" text NOT NULL, "content" text NOT NULL DEFAULT '', "tool_calls" jsonb NULL, "tool_call_id" text NOT NULL DEFAULT '', "sequence_number" integer NOT NULL, "forwarded_from" uuid NULL, "reply_to" uuid NULL, "created_at" timestamptz NOT NULL DEFAULT now(), "updated_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"));
-- Create index "idx_messages_session_seq" to table: "messages"
CREATE INDEX "idx_messages_session_seq" ON "public"."messages" ("session_id", "sequence_number");
-- Create "session_members" table
CREATE TABLE "public"."session_members" ("session_id" uuid NOT NULL, "user_id" uuid NOT NULL, "role" text NOT NULL DEFAULT 'guest', "joined_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("session_id", "user_id"));
-- Create "sessions" table
CREATE TABLE "public"."sessions" ("id" uuid NOT NULL DEFAULT gen_random_uuid(), "user_id" uuid NOT NULL, "title" text NOT NULL DEFAULT '', "model" text NOT NULL DEFAULT '', "system_prompt" text NOT NULL DEFAULT '', "context_window" integer NOT NULL DEFAULT 0, "context_messages" jsonb NOT NULL DEFAULT '[]', "context_summary" text NOT NULL DEFAULT '', "created_at" timestamptz NOT NULL DEFAULT now(), "updated_at" timestamptz NOT NULL DEFAULT now(), "deleted_at" timestamptz NULL, PRIMARY KEY ("id"));
-- Create "users" table
CREATE TABLE "public"."users" ("id" uuid NOT NULL DEFAULT gen_random_uuid(), "email" text NOT NULL, "name" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT now(), "updated_at" timestamptz NOT NULL DEFAULT now(), PRIMARY KEY ("id"), CONSTRAINT "users_email_key" UNIQUE ("email"));
