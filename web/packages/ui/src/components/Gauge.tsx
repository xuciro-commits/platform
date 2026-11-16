import { ProgressBar } from "@astryxdesign/core/ProgressBar";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { t } from "../i18n";

export function gaugeModel(value: number, max: number, warnAt?: number) {
  if (!Number.isFinite(value) || !Number.isFinite(max) || max <= 0 || warnAt !== undefined && !Number.isFinite(warnAt)) return;
  const fraction = Math.max(0, Math.min(1, value / max));
  return { fraction, danger: warnAt !== undefined && value >= warnAt, outside: value < 0 || value > max };
}

export function Gauge({ value, max, warnAt, label, suffix = "" }: { value: number; max: number; warnAt?: number; label: string; suffix?: string }) {
  const model = gaugeModel(value, max, warnAt);
  if (!model) return <Text as="p" role="alert">{t("Gauge needs a finite value and positive maximum.")}</Text>;
  const text = `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })}${suffix}`;
  return <VStack gap={1}>
    <Text type="label">{label}</Text>
    <ProgressBar label={label} value={Math.max(0, Math.min(max, value))} max={max} hasValueLabel formatValueLabel={() => text} variant={model.danger ? "error" : "success"} />
    <Text type="supporting">{t("of {max}", { max: `${max}${suffix}` })}</Text>
    {model.outside && <Text type="supporting" className="text-warning">{t("Gauge value is outside its display range.")}</Text>}
  </VStack>;
}
