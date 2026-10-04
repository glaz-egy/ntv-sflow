import type { Schemas } from "./common";

export type SubscribeMessage = Schemas["SubscribeMessage"];
export type ServerEventType = Schemas["ServerEventType"];
export type ServerEnvelope<T = unknown> = Omit<Schemas["ServerEnvelope"], "payload"> & { payload: T };
export type WindowUpdatePayload = Schemas["WindowUpdatePayload"];
