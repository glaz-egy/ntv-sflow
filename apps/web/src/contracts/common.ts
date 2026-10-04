/**
 * Wire contracts. Generated from api/openapi.yaml (the source of truth,
 * D-021) — regenerate with `pnpm gen:api`; never edit generated/ by hand.
 * This module only gives the generated schemas friendly names.
 */
import type { components } from "./generated/openapi";

export type Schemas = components["schemas"];

export type Timestamp = string;
export type IpAddress = string;
export type AddressFamily = Schemas["AddressFamily"];
export type MeasurementKind = Schemas["MeasurementKind"];
export type Measurement = Schemas["Measurement"];
export type TimeWindow = Schemas["TimeWindow"];

/**
 * Optional historical window (D-059): with both set, endpoints answer for
 * [start, end) from history; omit both for the live window.
 */
export interface TimeRangeQuery {
  start?: Timestamp | null;
  end?: Timestamp | null;
}
export type Direction = Schemas["Direction"];
export type DirectionFilter = Schemas["DirectionFilter"];
export type Protocol = Schemas["Protocol"];
export type ProtocolShare = Schemas["ProtocolShare"];
export type ObservationPoint = Schemas["ObservationPoint"];
export type ApiError = Schemas["ApiError"];
