import { useTranslation } from "react-i18next";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

/** 官方枚举由服务端提供,界面仅负责翻译,未知值保留原码。 */
export function SupportSelect({
  id,
  label,
  group,
  choices,
  value,
  onChange,
  optional,
  disabled,
}: {
  id: string;
  label: string;
  group: string;
  choices: string[];
  value: string;
  onChange: (value: string) => void;
  optional?: boolean;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="space-y-2">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      <Select
        value={value || (optional ? "__none" : "")}
        onValueChange={(v) => onChange(v === "__none" ? "" : v)}
        disabled={disabled}
      >
        <SelectTrigger id={id} className="h-11 rounded-xl">
          <SelectValue placeholder={t("support.choose")} />
        </SelectTrigger>
        <SelectContent>
          {optional && (
            <SelectItem value="__none">{t("support.unspecified")}</SelectItem>
          )}
          {choices.map((code) => (
            <SelectItem key={code} value={code}>
              {t(`support.${group}.${code}`, { defaultValue: code })}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
