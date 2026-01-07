# Glocker Refactoring - Gap Analysis

## Executive Summary

The refactored implementation is missing **8 out of 11 command-line flags** and the **default behavior** (no flags). This document compares the original `glocker.go` from master with the current `main.go` implementation.

---

## Section 1: Original Implementation (master branch)

### 1.1 All Command-Line Flags

| Flag | Type | Description | Handler Function |
|------|------|-------------|------------------|
| `-status` | bool | Show current status and configuration | `handleStatusCommand()` |
| `-enforce` | bool | Run enforcement loop (runs continuously) | Inline in main() |
| `-once` | bool | Run enforcement once and exit | Inline in main() |
| `-install` | bool | Install Glocker | Inline in main() |
| `-uninstall` | string | Uninstall Glocker and revert all changes (provide reason) | `handleUninstallRequest()` |
| `-reload` | bool | Reload configuration from config file | `handleReloadRequest()` |
| `-block` | string | Comma-separated list of hosts to add to always block list | `blockHostsFromFlag()` |
| `-unblock` | string | Comma-separated list of hosts to temporarily unblock (format: 'domain1,domain2:reason') | `unblockHostsFromFlag()` |
| `-add-keyword` | string | Comma-separated list of keywords to add to both URL and content keyword lists | `addKeywordsFromFlag()` |
| `-panic` | int | Enter panic mode for N minutes (suspends system and re-suspends on early wake) | `handlePanicRequest()` |
| `-lock` | bool | Immediately lock sudoers access (ignores time windows) | `handleLockRequest()` |

**Total: 11 flags**

### 1.2 Default Behavior (No Flags)

```go
// If no flags provided, check if glocker is running and show status or help
if flag.NFlag() == 0 {
    // Check if socket exists and is accessible
    if _, err := os.Stat(GLOCKER_SOCK); err == nil {
        // Socket exists, try to connect and get live status
        conn, err := net.Dial("unix", GLOCKER_SOCK)
        if err == nil {
            defer conn.Close()

            log.Println("=== LIVE STATUS ===")

            // Send status request
            conn.Write([]byte("status\n"))

            // Read response
            scanner := bufio.NewScanner(conn)
            for scanner.Scan() {
                line := scanner.Text()
                if line == "END" {
                    break
                }
                fmt.Println(line)
            }
            return
        }
    }

    // Socket not available, show help
    fmt.Println("Glocker - Domain and System Access Control")
    fmt.Println()
    fmt.Println("Usage:")
    flag.PrintDefaults()
    return
}
```

**Behavior:**
- If socket exists and daemon is running → Show live status from daemon
- Otherwise → Show help text and flag descriptions

### 1.3 Flag Handler Functions

All handlers communicate with the daemon via Unix socket at `/tmp/glocker.sock`.

#### `handleStatusCommand(config *Config)`
```go
// Check if socket exists and is accessible
if _, err := os.Stat(GLOCKER_SOCK); err == nil {
    // Try to connect and get live status
    conn, err := net.Dial("unix", GLOCKER_SOCK)
    if err == nil {
        defer conn.Close()
        log.Println("=== LIVE STATUS ===")
        conn.Write([]byte("status\n"))

        // Read response line by line until "END"
        scanner := bufio.NewScanner(conn)
        for scanner.Scan() {
            line := scanner.Text()
            if line == "END" {
                break
            }
            fmt.Println(line)
        }
        return
    }
}

// Socket not available, fall back to static status
log.Println("=== STATIC STATUS ===")
log.Println("(Service not running - showing configuration only)")
printConfig(config)
runOnce(config, true)
```

**Socket message format:** `"status\n"`
**Response format:** Multiple lines ending with `"END"`

#### `handleReloadRequest()`
```go
conn, err := net.Dial("unix", "/tmp/glocker.sock")
if err != nil {
    log.Fatalf("Failed to connect to glocker service: %v", err)
}
defer conn.Close()

message := "reload\n"
conn.Write([]byte(message))

// Read response
reader := bufio.NewReader(conn)
response, err := reader.ReadString('\n')
if err != nil {
    log.Fatalf("Failed to read response: %v", err)
}

log.Printf("Response: %s", strings.TrimSpace(response))
```

**Socket message format:** `"reload\n"`
**Response format:** Single line (e.g., `"OK: Reload request received\n"`)

#### `blockHostsFromFlag(config *Config, hostsStr string)`
```go
sendSocketMessage("block", hostsStr)
log.Println("Domains will be permanently blocked.")
```

**Socket message format:** `"block:domain1,domain2\n"`
**Response format:** Single line

#### `unblockHostsFromFlag(config *Config, hostsStr string)`
```go
// Parse the format: "domain1,domain2:reason"
parts := strings.SplitN(hostsStr, ":", 2)
if len(parts) != 2 {
    log.Fatal("ERROR: Reason required. Use format: 'domain1,domain2:reason'")
}

domains := strings.TrimSpace(parts[0])
reason := strings.TrimSpace(parts[1])

if domains == "" {
    log.Fatal("ERROR: No domains specified")
}

if reason == "" {
    log.Fatal("ERROR: Reason cannot be empty")
}

payload := fmt.Sprintf("%s:%s", domains, reason)
sendSocketMessageWithDetailedResponse("unblock", payload)
```

**Socket message format:** `"unblock:domain1,domain2:reason\n"`
**Response format:** Multiple lines with detailed feedback

#### `addKeywordsFromFlag(config *Config, keywordsStr string)`
```go
sendSocketMessage("add-keyword", keywordsStr)
log.Println("Keywords will be added to both URL and content keyword lists.")
```

**Socket message format:** `"add-keyword:keyword1,keyword2\n"`
**Response format:** Single line

#### `handlePanicRequest(config *Config, minutes int)`
```go
conn, err := net.Dial("unix", "/tmp/glocker.sock")
if err != nil {
    log.Fatalf("Failed to connect to glocker service: %v", err)
}
defer conn.Close()

message := fmt.Sprintf("panic:%d\n", minutes)

conn.Write([]byte(message))

// Read response
reader := bufio.NewReader(conn)
response, err := reader.ReadString('\n')
if err != nil {
    log.Fatalf("Failed to read response: %v", err)
}

log.Printf("%s", strings.TrimSpace(response))
```

**Socket message format:** `"panic:15\n"` (where 15 is minutes)
**Response format:** Single line (e.g., `"OK: Entering panic mode for 15 minutes\n"`)

#### `handleLockRequest()`
```go
sendSocketMessage("lock", "")
```

**Socket message format:** `"lock\n"`
**Response format:** Single line

#### `handleUninstallRequest(reason string)`
```go
conn, err := net.Dial("unix", "/tmp/glocker.sock")
if err != nil {
    log.Fatalf("Failed to connect to glocker service: %v", err)
}
defer conn.Close()

message := fmt.Sprintf("uninstall:%s\n", reason)

conn.Write([]byte(message))

// Read initial response
reader := bufio.NewReader(conn)
response, err := reader.ReadString('\n')
if err != nil {
    log.Fatalf("Failed to read initial response: %v", err)
}

log.Printf("Response: %s", strings.TrimSpace(response))

// Wait for completion signal
log.Println("Waiting for uninstall process to complete...")
completionResponse, err := reader.ReadString('\n')
if err != nil {
    log.Fatalf("Failed to read completion response: %v", err)
}

log.Printf("Completion: %s", strings.TrimSpace(completionResponse))
```

**Socket message format:** `"uninstall:reason\n"`
**Response format:** Two lines (initial acknowledgment + completion signal)

### 1.4 Socket Communication Helper Functions

#### `sendSocketMessage(action, domains string)`
- Connects to `/tmp/glocker.sock`
- Sends message in format: `"action:payload\n"` or `"action\n"` if no payload
- Reads single-line response
- Logs response and exits on error

#### `sendSocketMessageWithDetailedResponse(action, payload string)`
- Similar to above but reads multiple lines
- Used for operations that provide detailed feedback (e.g., unblock)

---

## Section 2: Current Implementation Gaps

### 2.1 Missing Command-Line Flags (8 of 11)

| Missing Flag | Type | Impact | Priority |
|--------------|------|--------|----------|
| `-enforce` | bool | Cannot run daemon continuously from CLI | **HIGH** |
| `-uninstall` | string | Cannot uninstall from CLI | **CRITICAL** |
| `-reload` | bool | Cannot reload config without restart | **HIGH** |
| `-block` | string | Cannot block domains from CLI | **HIGH** |
| `-unblock` | string | Cannot unblock domains from CLI | **HIGH** |
| `-add-keyword` | string | Cannot add keywords from CLI | **MEDIUM** |
| `-panic` | int | Cannot enter panic mode from CLI | **MEDIUM** |
| `-lock` | bool | Cannot lock sudoers from CLI | **MEDIUM** |

### 2.2 Implemented Flags (3 of 11)

| Flag | Status | Notes |
|------|--------|-------|
| `-install` | ✅ Works | Correctly implemented |
| `-once` | ✅ Works | Correctly implemented |
| `-status` | ⚠️ **BROKEN** | See Section 2.4 |

### 2.3 Missing Default Behavior (CRITICAL)

**Current behavior:** If no flags provided, `main.go` falls through to daemon mode (lines 68-123).

**Expected behavior:** Should check socket and show live status, or show help if daemon not running.

**Impact:** Running just `glocker` starts the daemon instead of showing status. This is confusing and dangerous.

### 2.4 Broken `-status` Flag

**Issue:** The `-status` flag uses `cli.GetStatusResponse(cfg)` and prints with `log.Print()`, but does not:
1. Try to connect to the socket first for live status
2. Read multi-line response until "END" marker
3. Fall back to static status if daemon not running

**Current implementation (main.go:56-60):**
```go
if *statusFlag {
    response := cli.GetStatusResponse(cfg)
    log.Print(response)
    return
}
```

**Expected implementation:**
```go
if *statusFlag {
    handleStatusCommand(cfg)  // Should use socket first, then fallback
    return
}
```

**Why it's garbled:** The `cli.GetStatusResponse()` function generates a formatted status report, but it's being called without the daemon running and without loading the current daemon state from the socket. The output is based on config file alone, not live status.

### 2.5 Missing Handler Functions

All flag handler functions are missing from `main.go`:

- ❌ `handleStatusCommand()` - Must try socket first, fallback to static
- ❌ `handleReloadRequest()` - Send reload message to socket
- ❌ `blockHostsFromFlag()` - Send block message to socket
- ❌ `unblockHostsFromFlag()` - Parse and validate, send unblock message
- ❌ `addKeywordsFromFlag()` - Send add-keyword message to socket
- ❌ `handlePanicRequest()` - Send panic message to socket
- ❌ `handleLockRequest()` - Send lock message to socket
- ❌ `handleUninstallRequest()` - Send uninstall message, wait for completion

### 2.6 Missing Socket Client Functions

The `main.go` lacks socket communication helpers:

- ❌ `sendSocketMessage(action, payload string)` - Basic socket message sender
- ❌ `sendSocketMessageWithDetailedResponse(action, payload string)` - Multi-line response handler

**Note:** These could be added to the `ipc` package as client functions.

### 2.7 Version Flag Issue

**Current:** Has `-version` flag (not in original)
**Original:** No version flag

**Decision:** Keep `-version` flag as it's a useful addition.

---

## Section 3: Summary of Gaps

### 3.1 Critical Issues (Must Fix Immediately)

1. **Missing default behavior** - Running `glocker` with no flags should show status, not start daemon
2. **Broken `-status` flag** - Output is garbled and doesn't try socket first
3. **Missing `-uninstall` flag** - No way to uninstall from CLI

### 3.2 High Priority Issues (Must Fix)

4. **Missing `-enforce` flag** - No way to run daemon from CLI (falls through automatically now)
5. **Missing `-reload` flag** - Cannot reload config without restart
6. **Missing `-block` flag** - Cannot block domains from CLI
7. **Missing `-unblock` flag** - Cannot unblock domains from CLI

### 3.3 Medium Priority Issues (Should Fix)

8. **Missing `-add-keyword` flag** - Cannot add keywords from CLI
9. **Missing `-panic` flag** - Cannot enter panic mode from CLI
10. **Missing `-lock` flag** - Cannot lock sudoers from CLI

### 3.4 Architecture Issues

11. **No socket client functions** - Need to add `SendSocketMessage()` and related functions to `ipc` package
12. **No flag handler functions** - Need to add all handler functions to `cli` package or `main.go`

---

## Section 4: Recommended Fix Strategy

### Phase 1: Fix Critical Issues (Priority: CRITICAL)

1. **Add default behavior** to `main.go`:
   - Check if `flag.NFlag() == 0`
   - Try to connect to socket and show live status
   - If socket unavailable, show help text

2. **Fix `-status` flag**:
   - Create `handleStatusCommand()` function
   - Try socket first, fallback to static status

3. **Add `-uninstall` flag**:
   - Create `handleUninstallRequest()` function
   - Send uninstall message to socket, wait for completion

### Phase 2: Add Missing Flags (Priority: HIGH)

4. **Add `-enforce` flag**:
   - Should explicitly run daemon mode
   - Current fallthrough behavior becomes explicit flag behavior

5. **Add `-reload` flag**:
   - Create `handleReloadRequest()` function

6. **Add `-block` flag**:
   - Create `blockHostsFromFlag()` function

7. **Add `-unblock` flag**:
   - Create `unblockHostsFromFlag()` function
   - Parse `"domains:reason"` format

### Phase 3: Add Remaining Flags (Priority: MEDIUM)

8. **Add `-add-keyword` flag**:
   - Create `addKeywordsFromFlag()` function

9. **Add `-panic` flag**:
   - Create `handlePanicRequest()` function

10. **Add `-lock` flag**:
    - Create `handleLockRequest()` function

### Phase 4: Refactor Socket Client Code (Priority: MEDIUM)

11. **Add socket client functions to `ipc` package**:
    - `SendSocketMessage(action, payload string) (string, error)`
    - `SendSocketMessageWithResponse(action, payload string) ([]string, error)`
    - Or keep in `main.go` as helper functions

---

## Section 5: Implementation Notes

### 5.1 Where to Put Handler Functions

**Option A:** Add all handlers to `main.go` (simpler, less refactoring)
- Pros: Quick to implement, keeps CLI entry point logic together
- Cons: Makes `main.go` larger

**Option B:** Add handlers to `cli` package (cleaner architecture)
- Pros: Better separation of concerns, more testable
- Cons: More refactoring needed

**Recommendation:** Start with Option A (add to `main.go`) for speed, refactor to Option B later if needed.

### 5.2 Socket Client Functions Location

**Option A:** Add to `ipc` package as client functions
- Pros: Proper separation (server in `ipc/server.go`, client in `ipc/client.go`)
- Cons: Need to handle both server and client in same package

**Option B:** Keep in `main.go` as helpers
- Pros: Simple, no package changes
- Cons: Not reusable

**Recommendation:** Option A - create `ipc/client.go` with socket client functions.

### 5.3 Testing Strategy

- Add tests for each handler function
- Mock socket communication for unit tests
- Integration tests with running daemon

---

## Section 6: File References

### Original Implementation
- `master:glocker.go:191-363` - Main function with all flags
- `master:glocker.go:3930-3980` - Handler functions

### Current Implementation
- `main.go:20-124` - Main function (only 4 flags)
- Missing all handler functions

### Daemon Socket Server (Already Implemented)
- `ipc/server.go:33-161` - Socket server and connection handler (✅ Complete)
- `ipc/server.go:HandleConnection()` - Handles all socket commands (✅ Complete)

### CLI Command Processors (Already Implemented)
- `cli/commands.go:15-141` - Status, reload, unblock, block, panic handlers (✅ Complete)

**Key Finding:** The daemon socket server and command processors are already fully implemented! We just need to add the CLI flags and handlers that send messages to the socket.

---

## Conclusion

The refactored implementation is **missing 73% of the CLI flags** and the **default behavior**. However, the daemon-side socket server and command processors are fully implemented in the `ipc` and `cli` packages.

**The fix is straightforward:**
1. Add missing flags to `main.go`
2. Add handler functions that send socket messages
3. Add default behavior (show status or help)
4. Create socket client functions in `ipc/client.go`

**Estimated effort:** 2-3 hours to implement all missing flags and handlers.
