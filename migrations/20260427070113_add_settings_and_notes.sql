-- Create "memories" table
CREATE TABLE "public"."memories" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "content" text NOT NULL,
  "session_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create "memory_sources" table
CREATE TABLE "public"."memory_sources" (
  "memory_id" uuid NOT NULL,
  "message_id" uuid NOT NULL,
  PRIMARY KEY ("memory_id", "message_id")
);
-- Create "settings_prompt" table
CREATE TABLE "public"."settings_prompt" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" text NOT NULL,
  "value" text NOT NULL DEFAULT '',
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "settings_prompt_name_key" UNIQUE ("name")
);
