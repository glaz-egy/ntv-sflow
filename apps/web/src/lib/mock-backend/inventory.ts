/**
 * Mock inventory (docs/MOCK_DATA.md §2–3). Mock-only values.
 * Internal addresses use 10.0.0.0/8 and ULA fd00::/8; no real user data.
 */
import type { DeviceAddress, DeviceType } from "@/contracts";
import type { Device, Exporter, Inventory, Network, TopologyLinkDef } from "./domain";

function v4(address: string, source: DeviceAddress["source"] = "dhcp"): DeviceAddress {
  return { address, family: "ipv4", source };
}
function v6(address: string, source: DeviceAddress["source"] = "nd"): DeviceAddress {
  return { address, family: "ipv6", source };
}

function device(
  id: string,
  displayName: string,
  type: DeviceType,
  addresses: DeviceAddress[],
  extra: Partial<Device> = {},
): Device {
  return {
    id,
    displayName,
    type,
    online: true,
    addresses,
    macs: [],
    vlanId: null,
    ssid: null,
    vendor: null,
    model: null,
    ...extra,
  };
}

export const MOCK_NETWORKS: Network[] = [
  { name: "mgmt", cidr: "10.0.0.0/24", vlanId: 1 },
  { name: "servers", cidr: "10.10.0.0/24", vlanId: 10 },
  { name: "clients", cidr: "10.20.0.0/24", vlanId: 20 },
  { name: "iot", cidr: "10.30.0.0/24", vlanId: 30 },
  { name: "wireless", cidr: "10.40.0.0/24", vlanId: 40 },
  { name: "servers-v6", cidr: "fd00:10::/64", vlanId: 10 },
  { name: "clients-v6", cidr: "fd00:20::/64", vlanId: 20 },
  { name: "wireless-v6", cidr: "fd00:40::/64", vlanId: 40 },
];

export const MOCK_DEVICES: Device[] = [
  device("dev_router", "home-router", "router", [v4("10.0.0.1", "static")], {
    vlanId: 1,
    vendor: "Mock Networks",
    model: "MR-1000",
    macs: ["02:00:00:00:00:01"],
  }),
  device("dev_core_sw", "core-switch", "switch", [v4("10.0.0.2", "static")], {
    vlanId: 1,
    vendor: "Mock Networks",
    model: "MS-24",
    macs: ["02:00:00:00:00:02"],
  }),
  device("dev_access_sw", "access-switch-01", "switch", [v4("10.0.0.3", "static")], {
    vlanId: 1,
    macs: ["02:00:00:00:00:03"],
  }),
  device("dev_ap_main", "ap-main", "wireless_ap", [v4("10.0.0.4", "static")], {
    vlanId: 1,
    macs: ["02:00:00:00:00:04"],
  }),
  device("dev_nas01", "nas-01", "server", [v4("10.10.0.10", "static"), v6("fd00:10::10", "static")], {
    vlanId: 10,
    vendor: "Mock Storage",
    macs: ["02:00:00:00:10:10"],
  }),
  device("dev_k8s01", "k8s-node-01", "kubernetes_node", [v4("10.10.0.20", "static")], {
    vlanId: 10,
    macs: ["02:00:00:00:10:20"],
  }),
  device("dev_media", "media-server", "server", [v4("10.10.0.30", "static")], {
    vlanId: 10,
    macs: ["02:00:00:00:10:30"],
  }),
  device("dev_pc01", "pc-01", "pc", [v4("10.20.0.10"), v6("fd00:20::10")], {
    vlanId: 20,
    macs: ["02:00:00:00:20:10"],
  }),
  device("dev_laptop01", "laptop-01", "pc", [v4("10.40.0.21")], {
    vlanId: 40,
    ssid: "home-5g",
    macs: ["02:00:00:00:40:21"],
  }),
  device("dev_phone01", "phone-01", "smartphone", [v4("10.40.0.22"), v6("fd00:40::22")], {
    vlanId: 40,
    ssid: "home-5g",
  }),
  // No "tablet" type exists in the device taxonomy; see docs/DECISIONS.md D-030.
  device("dev_tablet01", "tablet-01", "smartphone", [v4("10.40.0.23")], {
    vlanId: 40,
    ssid: "home-5g",
  }),
  device("dev_thermostat", "iot-thermostat", "iot", [v4("10.30.0.40")], {
    vlanId: 30,
    ssid: "home-iot",
  }),
  device("dev_camera", "iot-camera", "iot", [v4("10.30.0.41")], { vlanId: 30 }),
];

export const MOCK_EXPORTERS: Exporter[] = [
  {
    id: "exp_router",
    name: "home-router",
    agentAddress: "10.0.0.1",
    role: "boundary",
    samplingRate: 256,
    boundaryIfIndex: 1,
    ifSpeedBps: 10_000_000_000,
  },
  {
    id: "exp_core",
    name: "core-switch",
    agentAddress: "10.0.0.2",
    role: "core",
    samplingRate: 512,
    boundaryIfIndex: null,
    ifSpeedBps: 10_000_000_000,
  },
];

function link(
  a: string,
  b: string,
  evidence: TopologyLinkDef["evidence"],
  confidence: number,
  aInterface: string | null,
  bInterface: string | null,
  linkType: TopologyLinkDef["linkType"] = "physical",
): TopologyLinkDef {
  return { id: `link_${a}_${b}`, a, b, linkType, evidence, confidence, aInterface, bInterface };
}

export const INTERNET_NODE_ID = "internet";

export const MOCK_TOPOLOGY: TopologyLinkDef[] = [
  link(INTERNET_NODE_ID, "dev_router", "manual", 1, null, "wan0", "logical"),
  link("dev_router", "dev_core_sw", "manual", 1, "lan0", "Gi1/0/1"),
  link("dev_core_sw", "dev_nas01", "lldp", 0.95, "Gi1/0/3", "eth0"),
  link("dev_core_sw", "dev_k8s01", "lldp", 0.95, "Gi1/0/4", "eno1"),
  // Inferred from the MAC table only — must render as uncertain.
  link("dev_core_sw", "dev_media", "inferred", 0.5, "Gi1/0/5", null),
  link("dev_core_sw", "dev_access_sw", "lldp", 0.95, "Gi1/0/6", "Gi0/1"),
  link("dev_core_sw", "dev_ap_main", "lldp", 0.95, "Gi1/0/7", "eth0"),
  link("dev_access_sw", "dev_pc01", "manual", 1, "Gi0/2", null),
  link("dev_access_sw", "dev_camera", "inferred", 0.6, "Gi0/5", null),
  link("dev_ap_main", "dev_laptop01", "wlc", 0.9, "radio1", null, "wireless_association"),
  link("dev_ap_main", "dev_phone01", "wlc", 0.9, "radio1", null, "wireless_association"),
  link("dev_ap_main", "dev_tablet01", "wlc", 0.9, "radio1", null, "wireless_association"),
  link("dev_ap_main", "dev_thermostat", "wlc", 0.9, "radio0", null, "wireless_association"),
];

/** Core-switch ifIndex per directly attached device (mock). */
export const CORE_PORTS: Record<string, number> = {
  dev_router: 1,
  dev_nas01: 3,
  dev_k8s01: 4,
  dev_media: 5,
  dev_access_sw: 6,
  dev_ap_main: 7,
};

export function defaultInventory(): Inventory {
  return {
    devices: MOCK_DEVICES.map((d) => ({ ...d, addresses: [...d.addresses] })),
    networks: MOCK_NETWORKS,
    exporters: MOCK_EXPORTERS,
    topology: [...MOCK_TOPOLOGY],
    // Same values as configs/config.example.yaml network.internal_cidrs.
    internalCidrs: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"],
    origin: { label: "Home Network", latitude: 35.68, longitude: 139.76, precision: "city" },
    observationPolicy: {
      external: ["boundary", "core", "access"],
      internal: ["core", "access", "boundary"],
    },
  };
}
