env "local" {
  src = [
    "file://pkg/app/chat/db/schema.sql",
  ]
  dev = "docker://postgres/16/dev"
  migration {
    dir = "file://migrations"
  }
}
