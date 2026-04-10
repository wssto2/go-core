# go-core

## Overview

go-core is a modular Go backend framework that provides a shared foundation for building production-grade services.

It standardizes:

* Authentication & authorization
* Database access & transactions
* Validation & binding
* Error handling
* Middleware & HTTP layer
* Audit logging
* Multi-tenancy
* Event system
* Worker processes

The goal is to eliminate boilerplate and enforce **safe, predictable patterns** across all services.

---

## Key Principles

* Strong typing over dynamic behavior
* Explicit architecture (no hidden magic)
* Thin handlers, fat services
* Centralized error handling via `apperr`
* Context-driven request lifecycle
* Modular design (plug-and-play packages)

---

## Project Structure

### Core Packages

* `bootstrap` → application lifecycle, DI container, config
* `web` → HTTP handling, response format, helpers
* `auth` → authentication, JWT, policies, middleware
* `database` → repositories, transactions, types
* `validation` → input validation system
* `binders` → request binding (JSON, multipart)
* `middlewares` → HTTP middleware (auth, logging, security)
* `worker` → background workers

### Feature Packages

* `audit` → audit logging and diff tracking
* `datatable` → filtering, pagination, query helpers
* `tenancy` → multi-tenant context + DB scoping
* `event` → event bus abstraction

### Utility Packages

* `apperr` → structured application errors
* `i18n` → localization helpers
* `go2ts` → Go → TypeScript type generation
* `utils` → generic helpers

### Example App

* `go-core-example` → reference implementation

---

## How It Works

### Request Flow

1. HTTP request enters via `engine/gin.go`
2. Middleware chain executes (`middlewares`)
3. Request is bound using `binders`
4. Input validated via `validation`
5. Handler calls service layer
6. Service uses repositories (`database`)
7. Errors returned as `apperr.AppError`
8. Response formatted via `web/response`

---

## Error Handling

All errors use `apperr.AppError`:

* Structured error codes
* Log level hints
* Field-level validation errors

Example:

```go
return apperr.BadRequest("invalid input")
```

---

## Authentication

* JWT-based authentication
* Policy-based authorization (`namespace:action`)
* Context-based user injection

```go
user := auth.MustGetUser[MyUser](ctx)
```

---

## Database

* GORM-based repositories
* Transaction support via context
* Custom nullable & typed fields

---

## Audit Logging

* Tracks entity changes
* Stores before/after state
* Uses reflection-based diff

---

## Multi-Tenancy

* Context-based tenant resolution
* Optional DB query scoping

---

## Rules

* Do not bypass `apperr`
* Do not access DB outside repositories
* Do not put business logic in handlers
* Always pass `context.Context`
* Prefer interfaces over concrete types

---

## Purpose

This repository is a **core library**, not an application.

All business logic should live in consuming services.

---
