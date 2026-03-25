# Static Observer Extension

The static observer extension implements the `observer.Observable` interface for use with the
[`receiver_creator`](../../../receiver/receivercreator/README.md). Unlike other observers (k8s,
docker, host), it performs no dynamic discovery. Instead, it fires a single synthetic endpoint of
type `"static"` immediately on startup, causing the `receiver_creator` to instantiate all
matching subreceiver templates.

## Purpose

This extension solves the problem of setting per-receiver-instance resource attributes (such as
`service.name`) without modifying individual receiver components or creating one pipeline per
receiver instance.

## Configuration

No configuration is required.

```yaml
extensions:
  static_observer:
```

## Usage with receiver_creator

Use `rule: type == "static"` on each subreceiver template to match the synthetic endpoint.
Set static resource attributes directly in each subreceiver's `resource_attributes` block.

```yaml
extensions:
  static_observer:

receivers:
  receiver_creator:
    watch_observers: [static_observer]
    receivers:
      mysql/prod:
        rule: type == "static"
        config:
          endpoint: prod.rds.example.com:3306
          username: otel
          password: ${env:MYSQL_PASS_PROD}
          collection_interval: 5s
        resource_attributes:
          service.name: mysql-prod
          deployment.environment: production

      mysql/staging:
        rule: type == "static"
        config:
          endpoint: staging.rds.example.com:3306
          username: otel
          password: ${env:MYSQL_PASS_STAGING}
          collection_interval: 5s
        resource_attributes:
          service.name: mysql-staging
          deployment.environment: staging

exporters:
  otlp:
    endpoint: otel-backend.example.com:4317

service:
  extensions: [static_observer]
  pipelines:
    logs:
      receivers: [receiver_creator]
      exporters: [otlp]
    metrics:
      receivers: [receiver_creator]
      exporters: [otlp]
```

This scales to any number of receiver instances — each entry in `receivers:` under
`receiver_creator` gets its own `resource_attributes` without requiring a separate pipeline or
processor instance per receiver.

## How It Works

1. The `receiver_creator` calls `static_observer.ListAndWatch(notify)` on startup.
2. The static observer immediately calls `notify.OnAdd` with one synthetic endpoint
   (`ID: "static-0"`, `type: "static"`, empty `Target`).
3. For each subreceiver template whose `rule` matches `type == "static"`, the
   `receiver_creator` starts one subreceiver instance, wrapping its consumer with an
   `enhancingConsumer` that adds the configured `resource_attributes` onto all telemetry
   (only for keys not already set by the subreceiver).
4. `resource_attributes` values are plain strings — no backtick expressions needed.

## Behavior Notes

- `resource_attributes` are non-destructive: if the subreceiver itself already sets an attribute
  with the same key, the subreceiver's value takes precedence and the `resource_attributes` value
  is not applied.
- Since `Target` is empty, each subreceiver must specify its `endpoint` explicitly in `config:`.
- Multiple `receiver_creator` instances can each subscribe to the same `static_observer`.
