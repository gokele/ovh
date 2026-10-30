import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowRight,
  Check,
  CheckCircle2,
  CircleHelp,
  CreditCard,
  Loader2,
  Send,
  TriangleAlert,
  UserRound,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import {
  useSupportOptions,
  useSupportServices,
  useCreateSupportTicket,
} from "@/hooks/use-support";
import { SupportSelect } from "./SupportSelect";
import { SupportServicePicker } from "./SupportServicePicker";

const categoryIcons: Record<string, typeof CircleHelp> = {
  assistance: CircleHelp,
  billing: CreditCard,
  incident: TriangleAlert,
};

/** 分类、服务和正文分组呈现;草稿仅在用户确认提交时发送。 */
export function NewTicketDialog({
  accountId,
  onClose,
  onView,
}: {
  accountId: string;
  onClose: () => void;
  onView: (id: number) => void;
}) {
  const { t } = useTranslation();
  const meta = useSupportOptions(accountId);
  const create = useCreateSupportTicket(accountId);
  const [category, setCategory] = useState("");
  const [product, setProduct] = useState("");
  const [subcategory, setSubcategory] = useState("");
  const [serviceName, setServiceName] = useState("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const services = useSupportServices(accountId, product);
  const ready =
    !!meta.data &&
    !!category &&
    !!product &&
    !!subject.trim() &&
    !!body.trim() &&
    !create.isPending;
  /** 保留失败草稿,避免网络异常后自动重复创建工单。 */
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!ready) return;
    try {
      await create.mutateAsync({
        category,
        product,
        subcategory: subcategory || undefined,
        serviceName: serviceName.trim() || undefined,
        subject: subject.trim(),
        body: body.trim(),
      });
    } catch {
      /* Display inline. */
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !create.isPending) onClose();
      }}
    >
      <DialogContent
        className="flex flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl sm:p-0"
        style={{ paddingBottom: 0 }}
      >
        <DialogHeader className="shrink-0 border-b border-border px-5 pb-5 pt-6 sm:px-7 sm:pt-6">
          <DialogTitle className="pr-9 text-xl leading-7">
            {t("support.newTicket")}
          </DialogTitle>
          <DialogDescription className="leading-6">
            {t("support.createHint")}
          </DialogDescription>
        </DialogHeader>
        {meta.isPending ? (
          <p className="p-7" role="status">
            {t("support.loading")}
          </p>
        ) : meta.isError ? (
          <div className="p-7">
            <LoadFailed
              error={meta.error}
              onRetry={() => meta.refetch()}
              compact
            />
          </div>
        ) : create.isSuccess ? (
          <div className="px-7 py-8">
            <CheckCircle2
              aria-hidden="true"
              className="mb-4 h-10 w-10 text-success"
            />
            <p role="status" className="text-lg font-semibold">
              {t("support.created", {
                number:
                  create.data.result.ticketNumber ??
                  create.data.result.ticketId ??
                  "—",
              })}
            </p>
            {create.data.result.additionalNotice && (
              <p className="mt-3 whitespace-pre-wrap break-words text-sm leading-6 text-muted-foreground">
                {create.data.result.additionalNotice}
              </p>
            )}
            <div className="mt-7 flex flex-wrap gap-3">
              <Button
                className="h-11"
                onClick={() => onView(create.data.result.ticketId!)}
              >
                {t("support.view")}
                <ArrowRight aria-hidden="true" />
              </Button>
              <Button className="h-11" variant="outline" onClick={onClose}>
                {t("common.close")}
              </Button>
            </div>
          </div>
        ) : (
          meta.data && (
            <form onSubmit={submit} className="flex min-h-0 flex-col">
              <div className="space-y-5 overflow-y-auto px-5 py-6 sm:px-7">
                <div className="flex items-center gap-2 text-sm">
                  <UserRound
                    aria-hidden="true"
                    className="h-4 w-4 shrink-0 text-muted-foreground"
                  />
                  <span className="text-muted-foreground">
                    {t("support.account")}
                  </span>
                  <span
                    className="truncate font-medium"
                    title={meta.data.accountName || meta.data.accountId}
                  >
                    {meta.data.accountName || meta.data.accountId}
                  </span>
                </div>
                <fieldset disabled={create.isPending}>
                  <legend className="mb-3 text-sm font-semibold">
                    {t("support.categoryRequired")}
                  </legend>
                  <div className="grid gap-2 sm:grid-cols-3">
                    {meta.data.categories.map((code) => {
                      const Icon = categoryIcons[code] || CircleHelp;
                      return (
                        <label key={code} className="relative cursor-pointer">
                          <input
                            type="radio"
                            name="support-category"
                            value={code}
                            checked={category === code}
                            onChange={() => {
                              setCategory(code);
                              setSubcategory("");
                            }}
                            className="peer sr-only"
                            required
                          />
                          <div className="flex h-full items-start gap-3 rounded-xl border border-border p-3.5 transition-colors hover:bg-muted/40 peer-checked:border-foreground peer-checked:bg-muted/60 peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-focus-visible:ring-offset-2 peer-disabled:cursor-not-allowed peer-disabled:opacity-50">
                            <Icon
                              aria-hidden="true"
                              className="mt-0.5 h-5 w-5 shrink-0"
                            />
                            <div className="min-w-0">
                              <div className="flex items-center justify-between gap-2">
                                <span className="text-sm font-medium">
                                  {t(`support.categories.${code}`, {
                                    defaultValue: code,
                                  })}
                                </span>
                                {category === code && (
                                  <Check
                                    aria-hidden="true"
                                    className="h-3.5 w-3.5"
                                  />
                                )}
                              </div>
                              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                {t(`support.categoryHints.${code}`, {
                                  defaultValue: "",
                                })}
                              </p>
                            </div>
                          </div>
                        </label>
                      );
                    })}
                  </div>
                </fieldset>
                <div className="grid items-start gap-6 md:grid-cols-[260px_minmax(0,1fr)]">
                  <fieldset className="space-y-4" disabled={create.isPending}>
                    <legend className="mb-3 text-sm font-semibold">
                      {t("support.serviceSection")}
                    </legend>
                    <div className="grid items-start gap-4">
                      <SupportSelect
                        id="support-product"
                        label={t("support.productRequired")}
                        group="products"
                        choices={meta.data.products}
                        value={product}
                        onChange={(value) => {
                          setProduct(value);
                          setServiceName("");
                        }}
                        disabled={create.isPending}
                      />
                      <div className="space-y-2">
                        <label
                          htmlFor="support-service"
                          className="text-sm font-medium"
                        >
                          {t("support.service")}
                        </label>
                        <SupportServicePicker
                          value={serviceName}
                          onChange={setServiceName}
                          services={services.data || []}
                          disabled={create.isPending || !product}
                          loading={services.isFetching}
                        />
                        {services.isError && (
                          <p
                            role="status"
                            className="text-xs leading-5 text-warning"
                          >
                            {t("support.servicesFailed")}
                          </p>
                        )}
                      </div>
                    </div>
                    <details className="group">
                      <summary className="w-fit cursor-pointer rounded text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                        {t("support.moreClassification")}
                      </summary>
                      <div className="mt-3">
                        <SupportSelect
                          id="support-subcategory"
                          label={t("support.subcategory")}
                          group="subcategories"
                          choices={meta.data.subcategories}
                          value={subcategory}
                          onChange={setSubcategory}
                          optional
                          disabled={create.isPending}
                        />
                      </div>
                    </details>
                  </fieldset>
                  <fieldset
                    className="space-y-4 border-t border-border pt-6 md:border-l md:border-t-0 md:pl-6 md:pt-0"
                    disabled={create.isPending}
                  >
                    <legend className="sr-only">
                      {t("support.descriptionSection")}
                    </legend>
                    <p
                      id="support-language"
                      className="text-sm leading-6 text-muted-foreground"
                    >
                      {t("support.englishHint")}
                    </p>
                    <div className="space-y-2">
                      <label
                        htmlFor="support-subject"
                        className="text-sm font-medium"
                      >
                        {t("support.subjectRequired")}
                      </label>
                      <Input
                        id="support-subject"
                        lang="en"
                        spellCheck
                        aria-describedby="support-language"
                        className="h-11 rounded-xl text-base"
                        value={subject}
                        onChange={(event) => setSubject(event.target.value)}
                        placeholder={t("support.subjectPlaceholder")}
                        required
                      />
                    </div>
                    <div className="space-y-2">
                      <label
                        htmlFor="support-body"
                        className="text-sm font-medium"
                      >
                        {t("support.bodyRequired")}
                      </label>
                      <Textarea
                        id="support-body"
                        lang="en"
                        spellCheck
                        aria-describedby="support-language support-plain-text"
                        className="min-h-40 resize-y rounded-xl text-base leading-7"
                        value={body}
                        onChange={(event) => setBody(event.target.value)}
                        placeholder={t("support.bodyPlaceholder")}
                        required
                      />
                      <p
                        id="support-plain-text"
                        className="text-xs leading-5 text-muted-foreground"
                      >
                        {t("support.plainText")}
                      </p>
                    </div>
                  </fieldset>
                </div>
                {create.isError && (
                  <p
                    role="alert"
                    className="break-words text-sm leading-6 text-destructive"
                  >
                    {errorMessage(create.error)}
                  </p>
                )}
              </div>
              <div className="flex shrink-0 items-center justify-between gap-4 border-t border-border bg-muted/20 px-5 py-4 pb-[max(env(safe-area-inset-bottom),1rem)] sm:px-7">
                <Button
                  variant="ghost"
                  className="h-11"
                  type="button"
                  onClick={onClose}
                  disabled={create.isPending}
                >
                  {t("common.cancel")}
                </Button>
                <Button type="submit" className="h-11 px-6" disabled={!ready}>
                  {create.isPending ? (
                    <Loader2
                      aria-hidden="true"
                      className="animate-spin motion-reduce:animate-none"
                    />
                  ) : (
                    <Send aria-hidden="true" />
                  )}
                  {create.isPending
                    ? t("support.sending")
                    : t("support.submit")}
                </Button>
              </div>
            </form>
          )
        )}
      </DialogContent>
    </Dialog>
  );
}
