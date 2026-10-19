package config

// Field documents one setting in config.json. The configure and show-config
// commands, and the README table, all draw on this list.
type Field struct {
	Key     string // JSON path, for example "rate_limit.per_second"
	Meaning string
	Default string // shown to the user; "" means no default
}

// Fields lists every setting in the order it appears in config.json.
var Fields = []Field{
	{
		Key:     "listen",
		Meaning: "IP address and port the API listens on. Use the management NIC's IP rather than 0.0.0.0, so the API is only reachable on that network.",
		Default: DefaultListen,
	},
	{
		Key:     "api_keys",
		Meaning: "Clients allowed to call the API. Each entry has an id (written to the log, never the key itself) and the SHA-256 hash of the key. 'hvapi hash-key' generates a key and its hash. At least one is required.",
	},
	{
		Key:     "allowed_cidrs",
		Meaning: "Address ranges (CIDR) or single IPs allowed to connect. Any other address gets 403 before the key is checked. At least one is required.",
	},
	{
		Key:     "rate_limit.per_second",
		Meaning: "Sustained requests per second allowed for each key.",
		Default: "10",
	},
	{
		Key:     "rate_limit.burst",
		Meaning: "How many requests a key may make in a quick burst above the sustained rate.",
		Default: "20",
	},
	{
		Key:     "log_file",
		Meaning: "JSON log file. A relative path is next to config.json. Leave empty to log to the console only (as a service it falls back to hvapi.log).",
		Default: DefaultLogFile,
	},
	{
		Key:     "poll.state_seconds",
		Meaning: "Seconds between refreshes of fast-changing data such as VM state (used from phase 2).",
		Default: "10",
	},
	{
		Key:     "poll.heavy_seconds",
		Meaning: "Seconds between refreshes of slower data such as VM configuration and disks (used from phase 2).",
		Default: "60",
	},
	{
		Key:     "tls.cert_file",
		Meaning: "Reserved for HTTPS, which is not implemented yet. Must be empty.",
	},
	{
		Key:     "tls.key_file",
		Meaning: "Reserved for HTTPS, which is not implemented yet. Must be empty.",
	},
}

// Describe returns the meaning of a setting, or "" if the key is unknown.
func Describe(key string) string {
	for _, f := range Fields {
		if f.Key == key {
			return f.Meaning
		}
	}
	return ""
}
