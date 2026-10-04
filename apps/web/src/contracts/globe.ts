import type { DirectionFilter, Protocol, Schemas } from "./common";

export type Grouping = Schemas["Grouping"];
/** `<grouping>:<value>`; parse by the first colon only (IPv6 values contain colons). */
export type DestinationKey = string;
export type GlobeOrigin = Schemas["GlobeOrigin"];
export type LocationBasis = Schemas["LocationBasis"];
export type GeoLocation = Schemas["GeoLocation"];
export type GlobeDestination = Schemas["GlobeDestination"];
export type WanSummary = Schemas["WanSummary"];
export type GlobeResponse = Schemas["GlobeResponse"];
export type DestinationSource = Schemas["DestinationSource"];
export type DestinationMember = Schemas["DestinationMember"];
export type GlobeDestinationDetail = Schemas["GlobeDestinationDetail"];

/** Query parameters of GET /globe. */
export interface GlobeQuery {
  grouping: Grouping;
  source_node_id?: string | null;
  direction?: DirectionFilter;
  protocol?: Protocol | null;
  min_bps?: number;
  limit?: number;
}
