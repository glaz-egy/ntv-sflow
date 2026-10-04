"use client";

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

export interface Option<T extends string> {
  value: T;
  label: React.ReactNode;
  title?: string;
}

/** Single-choice segmented control (keyboard accessible via Radix). */
export function OptionGroup<T extends string>({
  value,
  onChange,
  options,
  ariaLabel,
}: {
  value: T;
  onChange: (value: T) => void;
  options: Option<T>[];
  ariaLabel?: string;
}) {
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      spacing={0}
      value={value}
      aria-label={ariaLabel}
      onValueChange={(v) => v && onChange(v as T)}
      className="w-full"
    >
      {options.map((o) => (
        <ToggleGroupItem key={o.value} value={o.value} title={o.title} className="flex-1 text-xs">
          {o.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

const NONE = "__none__";

export function SelectField<T extends string>({
  value,
  onChange,
  options,
  placeholder,
  allowNone,
  noneLabel = "Any",
  ariaLabel,
}: {
  value: T | null;
  onChange: (value: T | null) => void;
  options: Option<T>[];
  placeholder?: string;
  allowNone?: boolean;
  noneLabel?: string;
  ariaLabel?: string;
}) {
  return (
    <Select
      value={value ?? (allowNone ? NONE : undefined)}
      onValueChange={(v) => onChange(v === NONE ? null : (v as T))}
    >
      <SelectTrigger size="sm" className="w-full" aria-label={ariaLabel}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {allowNone && <SelectItem value={NONE}>{noneLabel}</SelectItem>}
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export const MIN_BPS_OPTIONS: Option<string>[] = [
  { value: "0", label: "All" },
  { value: "100000", label: "≥ 100 kb/s" },
  { value: "1000000", label: "≥ 1 Mb/s" },
  { value: "10000000", label: "≥ 10 Mb/s" },
  { value: "100000000", label: "≥ 100 Mb/s" },
];

export const PROTOCOL_OPTIONS: Option<"tcp" | "udp" | "icmp" | "other">[] = [
  { value: "tcp", label: "TCP" },
  { value: "udp", label: "UDP" },
  { value: "icmp", label: "ICMP" },
  { value: "other", label: "Other" },
];
