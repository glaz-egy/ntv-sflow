// Package contract holds the API wire DTOs (api/openapi.yaml).
//
// These are transport models only: JSON shapes, no behaviour. Domain types
// live in flow/devices/topology/aggregation and are mapped here by the
// projection package. Nullable fields are pointers without omitempty so they
// serialise as JSON null, matching the spec's `required` + `nullable`.
package contract

import "time"

// Timestamp serialises as RFC3339 UTC with millisecond precision.
type Timestamp time.Time

const timestampLayout = "2006-01-02T15:04:05.000Z07:00"

func (t Timestamp) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(t).UTC().Format(timestampLayout) + `"`), nil
}

func (t *Timestamp) UnmarshalJSON(b []byte) error {
	parsed, err := time.Parse(`"`+time.RFC3339Nano+`"`, string(b))
	if err != nil {
		return err
	}
	*t = Timestamp(parsed)
	return nil
}

func TS(t time.Time) Timestamp { return Timestamp(t) }

func TSPtr(t *time.Time) *Timestamp {
	if t == nil {
		return nil
	}
	v := Timestamp(*t)
	return &v
}

// ---------------------------------------------------------------- common

const (
	KindCounter         = "counter"
	KindSampledEstimate = "sampled_estimate"
)

// Measurement is a rate with its provenance (D-023).
type Measurement struct {
	Value           float64 `json:"value"`
	Unit            string  `json:"unit"`
	MeasurementKind string  `json:"measurement_kind"`
	WindowSeconds   *int    `json:"window_seconds,omitempty"`
	IntervalSeconds *int    `json:"interval_seconds,omitempty"`
	SampleCount     *int    `json:"sample_count,omitempty"`
}

type TimeWindow struct {
	Start Timestamp `json:"start"`
	End   Timestamp `json:"end"`
}

type ProtocolShare struct {
	Protocol string      `json:"protocol"`
	Port     *int        `json:"port"`
	Service  *string     `json:"service"`
	Bps      Measurement `json:"bps"`
}

type ObservationPoint struct {
	ExporterID       string `json:"exporter_id"`
	ExporterName     string `json:"exporter_name"`
	InputIfIndex     *int   `json:"input_if_index"`
	OutputIfIndex    *int   `json:"output_if_index"`
	UsedForAggregate bool   `json:"used_for_aggregate"`
	SamplingRate     int    `json:"sampling_rate"`
}

type ApiError struct {
	Error ApiErrorBody `json:"error"`
}

type ApiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------- status

type MockInfo struct {
	Seed     int     `json:"seed"`
	Scenario string  `json:"scenario"`
	Speed    float64 `json:"speed"`
}

type CollectorStatus struct {
	Status         string     `json:"status"`
	LastDatagramAt *Timestamp `json:"last_datagram_at"`
}

type StatusResponse struct {
	Mode                  string          `json:"mode"`
	Live                  bool            `json:"live"`
	Collector             CollectorStatus `json:"collector"`
	LastAggregateAt       *Timestamp      `json:"last_aggregate_at"`
	ServerTime            Timestamp       `json:"server_time"`
	UpdateIntervalSeconds float64         `json:"update_interval_seconds"`
	WindowSeconds         float64         `json:"window_seconds"`
	Mock                  *MockInfo       `json:"mock"`
}

// ----------------------------------------------------------------- globe

type GlobeOrigin struct {
	Label     string  `json:"label"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Precision string  `json:"precision"`
}

type GeoLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Basis     string  `json:"basis"`
}

type GlobeDestination struct {
	Key               string       `json:"key"`
	Grouping          string       `json:"grouping"`
	Label             string       `json:"label"`
	IP                *string      `json:"ip"`
	CountryCode       *string      `json:"country_code"`
	CountryName       *string      `json:"country_name"`
	City              *string      `json:"city"`
	Location          *GeoLocation `json:"location"`
	ASN               *int         `json:"asn"`
	Organization      *string      `json:"organization"`
	InboundBps        Measurement  `json:"inbound_bps"`
	OutboundBps       Measurement  `json:"outbound_bps"`
	SourceDeviceCount int          `json:"source_device_count"`
	SourceNodeIDs     []string     `json:"source_node_ids"`
	LastSeen          Timestamp    `json:"last_seen"`
}

type WanSummary struct {
	Download Measurement `json:"download"`
	Upload   Measurement `json:"upload"`
	Basis    string      `json:"basis"`
}

type GlobeResponse struct {
	Window         TimeWindow         `json:"window"`
	ServerTime     Timestamp          `json:"server_time"`
	Origin         GlobeOrigin        `json:"origin"`
	Grouping       string             `json:"grouping"`
	Destinations   []GlobeDestination `json:"destinations"`
	TruncatedCount int                `json:"truncated_count"`
	Summary        WanSummary         `json:"summary"`
}

type DestinationSource struct {
	NodeID      string      `json:"node_id"`
	Label       string      `json:"label"`
	DeviceType  string      `json:"device_type"`
	Resolved    bool        `json:"resolved"`
	InboundBps  Measurement `json:"inbound_bps"`
	OutboundBps Measurement `json:"outbound_bps"`
}

type DestinationMember struct {
	IP          string      `json:"ip"`
	City        *string     `json:"city"`
	CountryCode *string     `json:"country_code"`
	TotalBps    Measurement `json:"total_bps"`
}

type GlobeDestinationDetail struct {
	Destination       GlobeDestination    `json:"destination"`
	TopSources        []DestinationSource `json:"top_sources"`
	TopProtocols      []ProtocolShare     `json:"top_protocols"`
	Members           []DestinationMember `json:"members"`
	ObservationPoints []ObservationPoint  `json:"observation_points"`
}

// ------------------------------------------------------------------ home

type HomeNode struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Label       string       `json:"label"`
	DeviceType  string       `json:"device_type"`
	Status      string       `json:"status"`
	Addresses   []string     `json:"addresses"`
	VlanID      *int         `json:"vlan_id"`
	NetworkName *string      `json:"network_name"`
	RxBps       *Measurement `json:"rx_bps"`
	TxBps       *Measurement `json:"tx_bps"`
	LastSeen    *Timestamp   `json:"last_seen"`
}

type TrafficEdge struct {
	ID                string             `json:"id"`
	Source            string             `json:"source"`
	Target            string             `json:"target"`
	Scope             string             `json:"scope"`
	ForwardBps        Measurement        `json:"forward_bps"`
	ReverseBps        Measurement        `json:"reverse_bps"`
	TopProtocols      []ProtocolShare    `json:"top_protocols"`
	ObservationPoints []ObservationPoint `json:"observation_points"`
	LastSeen          Timestamp          `json:"last_seen"`
}

type HomeSummary struct {
	WanTotal          Measurement `json:"wan_total"`
	InternalEstimated Measurement `json:"internal_estimated"`
	DeviceCount       int         `json:"device_count"`
	OnlineCount       int         `json:"online_count"`
}

type DestinationContext struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	SourceNodeIDs []string `json:"source_node_ids"`
}

type HomeTrafficResponse struct {
	Window             TimeWindow          `json:"window"`
	ServerTime         Timestamp           `json:"server_time"`
	Nodes              []HomeNode          `json:"nodes"`
	Edges              []TrafficEdge       `json:"edges"`
	TruncatedCount     int                 `json:"truncated_count"`
	DestinationContext *DestinationContext `json:"destination_context"`
	Summary            HomeSummary         `json:"summary"`
}

type TopologyLink struct {
	ID              string  `json:"id"`
	SourceNodeID    string  `json:"source_node_id"`
	TargetNodeID    string  `json:"target_node_id"`
	LinkType        string  `json:"link_type"`
	Evidence        string  `json:"evidence"`
	Confidence      float64 `json:"confidence"`
	SourceInterface *string `json:"source_interface"`
	TargetInterface *string `json:"target_interface"`
}

type TopologyResponse struct {
	Nodes       []HomeNode     `json:"nodes"`
	Links       []TopologyLink `json:"links"`
	GeneratedAt Timestamp      `json:"generated_at"`
}

// --------------------------------------------------------------- devices

type DeviceAddress struct {
	Address string `json:"address"`
	Family  string `json:"family"`
	Source  string `json:"source"`
}

type DevicePeer struct {
	NodeID     string      `json:"node_id"`
	Label      string      `json:"label"`
	DeviceType string      `json:"device_type"`
	TxBps      Measurement `json:"tx_bps"`
	RxBps      Measurement `json:"rx_bps"`
}

type DeviceExternalDestination struct {
	Key         string      `json:"key"`
	Label       string      `json:"label"`
	CountryCode *string     `json:"country_code"`
	OutboundBps Measurement `json:"outbound_bps"`
	InboundBps  Measurement `json:"inbound_bps"`
}

type DeviceAttachment struct {
	ViaNodeID string  `json:"via_node_id"`
	ViaLabel  string  `json:"via_label"`
	Interface *string `json:"interface"`
	Evidence  string  `json:"evidence"`
}

type DeviceDetail struct {
	Node                    HomeNode                    `json:"node"`
	Addresses               []DeviceAddress             `json:"addresses"`
	Macs                    []string                    `json:"macs"`
	SSID                    *string                     `json:"ssid"`
	Attachment              *DeviceAttachment           `json:"attachment"`
	Vendor                  *string                     `json:"vendor"`
	Model                   *string                     `json:"model"`
	TopInternalPeers        []DevicePeer                `json:"top_internal_peers"`
	TopExternalDestinations []DeviceExternalDestination `json:"top_external_destinations"`
}

type DeviceListResponse struct {
	Devices []HomeNode `json:"devices"`
}

// ----------------------------------------------------------------- flows

type FlowRecord struct {
	SrcIP        string      `json:"src_ip"`
	DstIP        string      `json:"dst_ip"`
	Protocol     string      `json:"protocol"`
	SrcPort      *int        `json:"src_port"`
	DstPort      *int        `json:"dst_port"`
	Scope        string      `json:"scope"`
	Direction    *string     `json:"direction"`
	SrcNodeID    *string     `json:"src_node_id"`
	DstNodeID    *string     `json:"dst_node_id"`
	Bps          Measurement `json:"bps"`
	ExporterID   string      `json:"exporter_id"`
	SamplingRate int         `json:"sampling_rate"`
	LastSeen     *Timestamp  `json:"last_seen"`
}

type FlowSearchResponse struct {
	Window         TimeWindow   `json:"window"`
	Flows          []FlowRecord `json:"flows"`
	TruncatedCount int          `json:"truncated_count"`
}

// -------------------------------------------------------------- realtime

type SubscribeMessage struct {
	Type     string   `json:"type"`
	Channels []string `json:"channels"`
	Filters  *struct {
		SourceNodeID *string  `json:"source_node_id,omitempty"`
		MinBps       *float64 `json:"min_bps,omitempty"`
	} `json:"filters,omitempty"`
}

type ServerEnvelope struct {
	Type       string    `json:"type"`
	Sequence   int64     `json:"sequence"`
	ServerTime Timestamp `json:"server_time"`
	Payload    any       `json:"payload"`
}

type WindowUpdatePayload struct {
	WindowEnd Timestamp      `json:"window_end"`
	Status    StatusResponse `json:"status"`
}

// Ptr returns a pointer to v (for nullable fields).
func Ptr[T any](v T) *T { return &v }
