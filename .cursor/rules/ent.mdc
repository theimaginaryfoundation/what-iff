---
description: Rules for Ent ORM schema best practices
globs: ent/schema/*.go
alwaysApply: false
---
# Ent ORM Best Practices

This document outlines our best practices for using Ent as the ORM layer in this project.

## Schema Structure

- All entity schemas should implement the `ent.Schema` interface
- Always include descriptive comments for each schema type
- Implement the four main methods for each schema:
  - `Fields()`: Define all fields of the entity
  - `Edges()`: Define relationships to other entities
  - `Indexes()`: Define database indexes for query optimization
  - `Mixin()`: Include shared behaviors like timestamps

## Primary Keys

- Use UUID v4 as the primary key for all entities
- Always set UUIDs to be immutable and auto-generated:
```go
field.UUID("id", uuid.UUID{}).
    Default(uuid.New).
    Immutable()
```

## Field Validation

- Add appropriate validation to all fields:
  - Use `NotEmpty()` for required string fields
  - Use `Positive()` or `NonNegative()` for numeric fields that should be ≥ 0
  - Use `Range()` for fields with specific bounds (e.g., percentages)
- Mark optional fields with `Optional()` and provide sensible defaults with `Default()`

## Indexes

- Create indexes for fields commonly used in queries
- Use composite indexes for fields frequently queried together
- Always index foreign key fields and fields used for filtering

## Time Tracking

- Use the `TimeMixin` for all entities to track creation and update times
- Always include the TimeMixin in your schema's `Mixin()` method:
```go
func (YourEntity) Mixin() []ent.Mixin {
    return []ent.Mixin{
        TimeMixin{},
    }
}
```

## Edge Relationships

- Define clear and meaningful relationships between entities when applicable
- Document the cardinality and purpose of each relationship

## Code Style

- Use descriptive field and edge names
- Follow Go naming conventions for schema types (PascalCase)
- Keep field definitions clean and aligned for readability
