env "local" {
  src = [
    "file://pkg/app/chat/db/schema.sql",
    "file://pkg/app/password/db/schema.sql",
  ]
  dev = "docker://postgres/16/dev"
  migration {
    dir = "file://migrations"
  }
}
