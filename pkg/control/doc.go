// Package control is the versioned control-plane contract for wddl.
//
// The command tree is:
//
//	wddl run
//	wddl status [--json]
//	wddl watch [--json]
//	wddl scan remote|local|all [--json]
//	wddl queue list [--json]
//	wddl queue remove <id> [--json]
//	wddl queue retry <id> [--json]
//	wddl download cancel <id> [--json]
//	wddl id <remote-path> [--json]
//	wddl cleanup remote [--confirm <token>] [--json]
//	wddl config validate [--json]
//	wddl config reload [--json]
//	wddl version [--json]
//
// Commands that reach the daemon use HTTP over its configured Unix socket.
// Validate and version are intentionally standalone. Data-producing commands
// support a stable JSON representation; operational failures return a non-zero
// process status.
package control
