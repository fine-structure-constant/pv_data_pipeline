# SQL / Migration

This project currently uses GORM `AutoMigrate` from:

```bash
go run ./cmd/pvsk migrate
```

The model source of truth is `internal/models/models.go`.

Future hand-written SQL migrations can be added here if the schema needs
destructive changes, backfills, indexes that GORM cannot express cleanly, or
database-specific views for AI training exports.
