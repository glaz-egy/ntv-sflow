import type { Protocol, Schemas, TimeRangeQuery } from "./common";
import type { DestinationKey } from "./globe";

export type DeviceType = Schemas["DeviceType"];
export type DeviceStatus = Schemas["DeviceStatus"];
export type HomeNodeKind = Schemas["HomeNodeKind"];
export type HomeNode = Schemas["HomeNode"];
export type TrafficScope = Schemas["TrafficScope"];
export type ScopeFilter = Schemas["ScopeFilter"];
export type TrafficEdge = Schemas["TrafficEdge"];
export type HomeSummary = Schemas["HomeSummary"];
export type DestinationContext = Schemas["DestinationContext"];
export type HomeTrafficResponse = Schemas["HomeTrafficResponse"];
export type TopologyLinkType = Schemas["TopologyLinkType"];
export type TopologyEvidence = Schemas["TopologyEvidence"];
export type TopologyLink = Schemas["TopologyLink"];
export type TopologyResponse = Schemas["TopologyResponse"];
export type DeviceAddress = Schemas["DeviceAddress"];
export type DevicePeer = Schemas["DevicePeer"];
export type DeviceExternalDestination = Schemas["DeviceExternalDestination"];
export type DeviceAttachment = Schemas["DeviceAttachment"];
export type DeviceDetail = Schemas["DeviceDetail"];
export type DeviceListResponse = Schemas["DeviceListResponse"];

/** Query parameters of GET /home/traffic. */
export interface HomeTrafficQuery extends TimeRangeQuery {
  focus_node_id?: string | null;
  destination_key?: DestinationKey | null;
  vlan_id?: number | null;
  device_types?: DeviceType[] | null;
  protocol?: Protocol | null;
  min_bps?: number;
  limit?: number;
  include_inactive?: boolean;
  scope?: ScopeFilter;
}
