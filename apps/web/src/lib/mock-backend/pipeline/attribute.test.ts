import { describe, expect, it } from "vitest";
import type { ObservationPolicy, WindowFlowObservation } from "../domain";
import { MOCK_DEVICES, MOCK_EXPORTERS } from "../inventory";
import { CidrClassifier } from "../net/ip";
import { attributeFlows, DeviceRegistry } from "./attribute";

function obs(partial: Partial<WindowFlowObservation>): WindowFlowObservation {
  return {
    exporterId: "exp_core",
    inputIfIndex: null,
    outputIfIndex: null,
    srcIp: "10.20.0.10",
    dstIp: "198.51.100.10",
    protocol: "tcp",
    srcPort: 50000,
    dstPort: 443,
    samplingRate: 512,
    sampleCount: 10,
    estimatedBytes: 10 * 1000 * 512,
    lastSampleAt: 5,
    ...partial,
  };
}

const ctx = {
  classifier: new CidrClassifier(["10.0.0.0/8", "fc00::/7"]),
  registry: new DeviceRegistry(MOCK_DEVICES),
  geo: new Map(),
  exporters: MOCK_EXPORTERS,
  policy: {
    external: ["boundary", "core", "access"],
    internal: ["core", "access", "boundary"],
  } as ObservationPolicy,
};

describe("attributeFlows — observation semantics", () => {
  it("does not sum the same flow seen at two exporters", () => {
    const core = obs({ exporterId: "exp_core", samplingRate: 512, sampleCount: 10, estimatedBytes: 5_120_000 });
    const router = obs({ exporterId: "exp_router", samplingRate: 256, sampleCount: 21, estimatedBytes: 5_376_000 });
    const [flow, ...rest] = attributeFlows([core, router], ctx);
    expect(rest).toHaveLength(0);
    // External traffic prefers the boundary exporter, not the sum or the max.
    expect(flow!.used.exporterId).toBe("exp_router");
    expect(flow!.estimatedBytes).toBe(5_376_000);
    expect(flow!.observations).toHaveLength(2);
  });

  it("prefers the core exporter for internal flows", () => {
    const internal = { srcIp: "10.20.0.10", dstIp: "10.10.0.10", dstPort: 445 };
    const flows = attributeFlows(
      [obs({ ...internal, exporterId: "exp_router" }), obs({ ...internal, exporterId: "exp_core" })],
      ctx,
    );
    expect(flows[0]!.used.exporterId).toBe("exp_core");
    expect(flows[0]!.scope).toBe("internal");
  });
});

describe("attributeFlows — classification and identity", () => {
  it("derives direction from internal/external classification", () => {
    const [out] = attributeFlows([obs({})], ctx);
    expect(out!.direction).toBe("outbound");
    const [inb] = attributeFlows([obs({ srcIp: "198.51.100.10", dstIp: "10.20.0.10" })], ctx);
    expect(inb!.direction).toBe("inbound");
    expect(inb!.internalNodeId).toBe("dev_pc01");
  });

  it("resolves IPv4 and IPv6 addresses of one device to the same identity", () => {
    const flows = attributeFlows(
      [obs({ srcIp: "fd00:20::10", dstIp: "2001:db8:1::10" }), obs({})],
      ctx,
    );
    expect(new Set(flows.map((f) => f.internalNodeId))).toEqual(new Set(["dev_pc01"]));
  });

  it("keeps unresolved internal endpoints with a temporary identity", () => {
    const [f] = attributeFlows([obs({ srcIp: "10.30.0.77" })], ctx);
    expect(f!.internalNodeId).toBe("ep:10.30.0.77");
  });

  it("leaves unknown geo unknown", () => {
    const [f] = attributeFlows([obs({ dstIp: "192.0.2.250" })], ctx);
    expect(f!.geo.latitude).toBeNull();
    expect(f!.geo.asn).toBeNull();
  });
});
