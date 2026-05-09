package callbacks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// DatadogCallback ships a JSON log line to the Datadog Logs intake AND
// (when StatsdAddr is set) emits the same metric names exposed by
// /metrics over DogStatsD UDP. The dual path means dashboards work
// regardless of whether the customer runs the Datadog Agent locally
// or only allows outbound HTTPS.
type DatadogCallback struct {
	endpoint   string
	apiKey     string
	client     *http.Client
	statsdAddr string
	statsdConn *net.UDPConn
	statsdMu   sync.Mutex
	tags       []string
}

func NewDatadog(endpoint, apiKeyEnv, site string) *DatadogCallback {
	if endpoint == "" {
		if site == "" {
			site = "datadoghq.com"
		}
		endpoint = fmt.Sprintf("https://http-intake.logs.%s/api/v2/logs", site)
	}
	return &DatadogCallback{
		endpoint: endpoint,
		apiKey:   os.Getenv(apiKeyEnv),
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// WithStatsd enables DogStatsD/UDP metric emission. addr is the agent
// socket (typically "127.0.0.1:8125"). Extra tags are appended to
// every metric. The connection is lazily dialed to keep startup
// failure-tolerant; a misconfigured agent only burns log lines.
func (d *DatadogCallback) WithStatsd(addr string, extraTags ...string) *DatadogCallback {
	if addr == "" {
		return d
	}
	d.statsdAddr = addr
	d.tags = append(d.tags, extraTags...)
	return d
}

func (d *DatadogCallback) Name() string { return "datadog" }

func (d *DatadogCallback) Send(ctx context.Context, event RequestEvent) error {
	d.emitStatsd(event)

	msg := map[string]interface{}{
		"trace_id":          event.TraceID,
		"model":             event.ModelAlias,
		"provider":          event.Provider,
		"endpoint":          event.Endpoint,
		"status":            event.Status,
		"prompt_tokens":     event.PromptTokens,
		"completion_tokens": event.CompletionTokens,
		"total_tokens":      event.TotalTokens,
		"cost_usd":          event.CostUSD,
		"duration_ms":       event.DurationMS,
	}
	// Carry the moat data into the log payload so a Datadog log search
	// returns the same drill-down a /savings UI row would.
	if event.Routing != nil {
		msg["routing"] = event.Routing
	}
	if event.Cost != nil {
		msg["cost"] = event.Cost
	}
	if event.Quality != nil {
		msg["quality"] = event.Quality
	}
	if event.Receipt != nil {
		msg["receipt"] = event.Receipt
	}

	entry := []map[string]interface{}{
		{
			"ddsource": "gateway-llm",
			"ddtags":   fmt.Sprintf("model:%s,provider:%s,endpoint:%s", event.ModelAlias, event.Provider, event.Endpoint),
			"hostname": "gateway-llm-gateway",
			"service":  "gateway-llm",
			"message":  msg,
		},
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", d.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", d.apiKey)
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("datadog send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("datadog returned status %d", resp.StatusCode)
	}
	return nil
}

func (d *DatadogCallback) Close() error {
	d.statsdMu.Lock()
	defer d.statsdMu.Unlock()
	if d.statsdConn != nil {
		_ = d.statsdConn.Close()
		d.statsdConn = nil
	}
	return nil
}

// ---- DogStatsD wire protocol ----------------------------------------
// We hand-roll the small subset of the protocol we need rather than
// pull in datadog-go just to write 11 lines per request. Format:
//   metric_name:value|type|#tag1:v1,tag2:v2
// where type is c (counter), g (gauge), h (histogram), or ms (timing).

func (d *DatadogCallback) emitStatsd(event RequestEvent) {
	if d.statsdAddr == "" {
		return
	}
	conn := d.dialStatsd()
	if conn == nil {
		return
	}

	baseTags := []string{
		"model:" + safeTag(event.ModelAlias),
		"provider:" + safeTag(event.Provider),
		"status:" + statusBucket(event.Status),
	}
	baseTags = append(baseTags, d.tags...)

	d.send(conn, "gatewayllm.requests:1|c", baseTags)
	if event.DurationMS > 0 {
		d.send(conn, fmt.Sprintf("gatewayllm.request_duration_ms:%d|h", event.DurationMS), baseTags)
	}
	if event.PromptTokens > 0 {
		d.send(conn, fmt.Sprintf("gatewayllm.tokens:%d|c", event.PromptTokens), append(append([]string{}, baseTags...), "kind:prompt"))
	}
	if event.CompletionTokens > 0 {
		d.send(conn, fmt.Sprintf("gatewayllm.tokens:%d|c", event.CompletionTokens), append(append([]string{}, baseTags...), "kind:completion"))
	}
	if event.CostUSD > 0 {
		d.send(conn, fmt.Sprintf("gatewayllm.cost_usd:%f|c", event.CostUSD), baseTags)
	}

	if event.Routing == nil {
		return
	}
	moatTags := []string{
		"alias:" + safeTag(event.Routing.RequestedAlias),
		"served_alias:" + safeTag(firstNonEmpty(event.Routing.ServedAlias, event.Routing.RequestedAlias)),
		"strategy:" + safeTag(event.Routing.Strategy),
		"retried:" + boolTag(event.Routing.Retried),
		"org_id:" + safeTag(firstNonEmpty(event.OrgID, "global")),
	}
	moatTags = append(moatTags, d.tags...)

	d.send(conn, "gatewayllm.moat.requests:1|c", moatTags)
	if event.Cost != nil {
		costTags := []string{
			"alias:" + safeTag(event.Routing.RequestedAlias),
			"served_alias:" + safeTag(firstNonEmpty(event.Routing.ServedAlias, event.Routing.RequestedAlias)),
			"org_id:" + safeTag(firstNonEmpty(event.OrgID, "global")),
		}
		costTags = append(costTags, d.tags...)
		if event.Cost.BaselineUSD > 0 {
			d.send(conn, fmt.Sprintf("gatewayllm.moat.baseline_cost_usd:%f|c", event.Cost.BaselineUSD), costTags)
		}
		if event.Cost.ActualUSD > 0 {
			d.send(conn, fmt.Sprintf("gatewayllm.moat.cost_usd:%f|c", event.Cost.ActualUSD), costTags)
		}
		if event.Cost.SavingsUSD > 0 {
			d.send(conn, fmt.Sprintf("gatewayllm.moat.savings_usd:%f|c", event.Cost.SavingsUSD), costTags)
		}
	}
	if event.Quality != nil {
		qTags := []string{"alias:" + safeTag(event.Routing.RequestedAlias)}
		qTags = append(qTags, d.tags...)
		if event.Quality.Score != nil {
			d.send(conn, fmt.Sprintf("gatewayllm.moat.quality_score:%f|h", *event.Quality.Score), qTags)
		}
		if event.Quality.Pass != nil {
			result := "fail"
			if *event.Quality.Pass {
				result = "pass"
			}
			d.send(conn, "gatewayllm.moat.judge_passes:1|c", append(qTags, "result:"+result))
		}
	}
	decision := "passthrough"
	switch {
	case event.Routing.Retried:
		decision = "retried"
	case event.Routing.Overridden:
		decision = "overridden"
	}
	d.send(conn, "gatewayllm.moat.routing_decisions:1|c", append([]string{"decision:" + decision}, d.tags...))
}

func (d *DatadogCallback) dialStatsd() *net.UDPConn {
	d.statsdMu.Lock()
	defer d.statsdMu.Unlock()
	if d.statsdConn != nil {
		return d.statsdConn
	}
	addr, err := net.ResolveUDPAddr("udp", d.statsdAddr)
	if err != nil {
		return nil
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil
	}
	d.statsdConn = conn
	return conn
}

func (d *DatadogCallback) send(conn *net.UDPConn, body string, tags []string) {
	line := body
	if len(tags) > 0 {
		line += "|#" + strings.Join(tags, ",")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	_, _ = conn.Write([]byte(line))
}

func safeTag(s string) string {
	if s == "" {
		return "unknown"
	}
	// DogStatsD tags can't contain whitespace or commas; replace
	// aggressively rather than silently dropping samples.
	r := strings.NewReplacer(" ", "_", ",", "_", "|", "_", "#", "_", ":", "_")
	return r.Replace(s)
}

func boolTag(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func statusBucket(code int) string {
	switch {
	case code <= 0:
		return "unknown"
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}
