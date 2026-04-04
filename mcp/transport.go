package mcp

// transport abstracts the communication layer between the MCP client and server.
// Implementations: stdioTransport (subprocess), sseTransport (HTTP SSE).
type transport interface {
	// Start initializes the transport (launch process, connect SSE, etc.)
	Start() error

	// WriteRequest sends a JSON-RPC request to the server.
	WriteRequest(data []byte) error

	// ReadLoop reads responses from the server and dispatches them via onMessage.
	// Blocks until the connection is closed or an error occurs.
	// Must call onMessage for each complete JSON-RPC response received.
	ReadLoop(onMessage func([]byte))

	// Close shuts down the transport.
	Close() error
}
