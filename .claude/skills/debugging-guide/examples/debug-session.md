# Example: Debugging a 500 Internal Server Error

This annotated example shows a realistic debugging session for an Extend Service Extension
(Go) app where `GetGuildProgress` returns `500 Internal Server Error`.

---

## The Report

A developer sends this message:

> *"My service is running but when I call GET `/guild/v1/admin/namespace/mygame/guilds/guild_001/progress`
> I get 500. The service started fine."*

---

## Step 1 — Collect logs

Ask for or check the log output. The developer shares:

```json
{"time":"2026-03-10T09:12:01Z","level":"INFO","msg":"started call","grpc.service":"service.Service","grpc.method":"GetGuildProgress"}
{"time":"2026-03-10T09:12:01Z","level":"ERROR","msg":"finished call","grpc.code":"Internal","grpc.method":"GetGuildProgress","error":"Error getting guild progress: [GET /cloudsave/v1/admin/namespaces/{namespace}/records/{key}][404] adminGetGameRecordHandlerV1NotFound"}
```

**What this tells us:**
- The request successfully passed auth (we see `started call`).
- The gRPC call reached `GetGuildProgress` in `myService.go`.
- CloudSave returned a `404 Not Found` for the key `guildProgress_guild_001`.
- The storage layer wrapped this 404 as `codes.Internal` and propagated it as a 500.

---

## Step 2 — Read the storage layer

Looking at `pkg/storage/storage.go`:

```go
func (c *CloudsaveStorage) GetGuildProgress(...) (*pb.GuildProgress, error) {
    // ...
    response, err := c.csStorage.AdminGetGameRecordHandlerV1Short(input)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "Error getting guild progress: %v", err)
    }
    // ...
}
```

**Problem identified:** Any CloudSave error — including a legitimate "record not found" —
is returned as `codes.Internal`. This is incorrect: a missing record should be `codes.NotFound`.

---

## Step 3 — Confirm with a breakpoint (optional)

If the log alone isn't conclusive, set a breakpoint at the `if err != nil` line in
`GetGuildProgress` inside `pkg/storage/storage.go`. Inspect the `err` value in the
Variables panel to see the exact CloudSave error type.

---

## Step 4 — The fix

The correct behaviour is to distinguish "not found" from "actual internal error".
In `pkg/storage/storage.go`:

```go
// Before
if err != nil {
    return nil, status.Errorf(codes.Internal, "Error getting guild progress: %v", err)
}

// After
if err != nil {
    // Check if CloudSave returned a 404
    if strings.Contains(err.Error(), "NotFound") {
        return nil, status.Errorf(codes.NotFound, "Guild progress not found: %v", err)
    }
    return nil, status.Errorf(codes.Internal, "Error getting guild progress: %v", err)
}
```

---

## Step 5 — Verify

```bash
# The response should now be 404, not 500
curl -s -o /dev/null -w "%{http_code}" \
  http://localhost:8000/guild/v1/admin/namespace/mygame/guilds/nonexistent/progress
# Expected: 404
```

And in the logs:
```json
{"grpc.code":"NotFound","grpc.method":"GetGuildProgress","msg":"finished call"}
```

---

## Key takeaways from this session

1. **Read the log first** — the gRPC middleware already logs the error code and message.
   You often don't need a breakpoint to identify the layer where the failure occurred.
2. **Follow the error upward** — the HTTP 500 came from `codes.Internal` which came from
   a CloudSave 404. Each layer added wrapping; trace it back.
3. **Distinguish error types** — returning `codes.Internal` for every storage error hides
   useful information from callers. Map known error conditions to appropriate gRPC codes.
