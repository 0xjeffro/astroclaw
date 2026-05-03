env "local" {
  src = [
    "file://pkg/app/chat/db/schema.sql",
    "file://pkg/app/passwords/db/schema.sql",
    "file://pkg/app/settings/db/schema.sql",
    "file://pkg/app/notes/db/schema.sql",
    "file://pkg/app/agents/db/schema.sql",
  ]
  dev = "docker://postgres/16/dev"
  migration {
    dir = "file://migrations"
  }
}
