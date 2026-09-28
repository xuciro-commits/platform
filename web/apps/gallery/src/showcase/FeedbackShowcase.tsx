import {
  NotificationList, PageHeader, StatusTag, Tag, defineStatuses, notify, submissionStatuses, type NotificationItem,
} from "@platform/ui";
import { useState } from "react";
import { ShowcaseCard } from "./ShowcaseCard";

const orderStatus = defineStatuses({
  draft: { label: "Draft Order", tone: "neutral" },
  released: { label: "Released", tone: "info" },
  in_progress: { label: "In Production", tone: "warning" },
  inspection: { label: "QA Inspection", tone: "info" },
  completed: { label: "Finished", tone: "success" },
  scrapped: { label: "Scrapped / Refused", tone: "danger" },
});

const initialNotifications: NotificationItem[] = [
  {
    id: "n-1",
    title: "Engineering change notice ECN-2026-44 approved",
    body: "Drawing DWG-401 for Pump Shaft Blank updated to Rev 3 by Chief Engineer.",
    at: new Date(Date.now() - 1000 * 60 * 18).toISOString(),
    read: false,
    app: "mes",
  },
  {
    id: "n-2",
    title: "Critical warning: CNC-02 spindle temperature high",
    body: "Spindle temperature exceeded 76.5°C on sensor TMP-002. Routing diverted to buffer queue.",
    at: new Date(Date.now() - 1000 * 60 * 55).toISOString(),
    read: false,
    app: "iot",
  },
  {
    id: "n-3",
    title: "Quarterly stock inventory audit completed",
    body: "99.82% variance match achieved across Warehouse B and Raw Materials Depot.",
    at: new Date(Date.now() - 1000 * 60 * 60 * 5).toISOString(),
    read: true,
    app: "erp",
  },
];

export function FeedbackShowcase() {
  const [notifications, setNotifications] = useState(initialNotifications);

  return (
    <div className="flex flex-col gap-6 pb-12">
      <PageHeader
        title="Status & Feedback"
        description="Standardized semantic status badges, lifecycle registries, and interactive notification feeds."
      />

      {/* Semantic Tones */}
      <ShowcaseCard
        title="Tag / Five Semantic Tones"
        description="Tailored Oklch design tokens guaranteed to maintain contrast and harmony across light and dark modes."
      >
        <div className="flex flex-wrap items-center gap-2">
          <Tag label="Neutral Tone" tone="neutral" />
          <Tag label="Info Tone" tone="info" />
          <Tag label="Success Tone" tone="success" />
          <Tag label="Warning Tone" tone="warning" />
          <Tag label="Danger Tone" tone="danger" />
          <Tag label="Outline Neutral" />
        </div>
      </ShowcaseCard>

      {/* StatusTag Registry */}
      <div className="grid gap-4 md:grid-cols-2">
        <ShowcaseCard
          title="StatusTag / Manufacturing Lifecycle"
          description="Type-safe status mapping defined via defineStatuses() with localized labels and automatic tone association."
          contentClassName="flex flex-wrap gap-2"
        >
          <StatusTag status="draft" registry={orderStatus} />
          <StatusTag status="released" registry={orderStatus} />
          <StatusTag status="in_progress" registry={orderStatus} />
          <StatusTag status="inspection" registry={orderStatus} />
          <StatusTag status="completed" registry={orderStatus} />
          <StatusTag status="scrapped" registry={orderStatus} />
        </ShowcaseCard>

        <ShowcaseCard
          title="StatusTag / Standard Submission Lifecycle"
          description="Pre-configured submissionStatuses for approval chains, leave requests, and document governance."
          contentClassName="flex flex-wrap gap-2"
        >
          <StatusTag status="draft" registry={submissionStatuses} />
          <StatusTag status="submitted" registry={submissionStatuses} />
          <StatusTag status="approved" registry={submissionStatuses} />
          <StatusTag status="rejected" registry={submissionStatuses} />
        </ShowcaseCard>
      </div>

      {/* Notification Center */}
      <ShowcaseCard
        title="Notification Center (NotificationList)"
        description="Unread indicators (blue dot), timestamps, origin applications, and live mark-as-read interaction."
        contentClassName="w-full"
      >
        <div className="w-full max-w-2xl">
          <NotificationList
            items={notifications}
            onRead={(item) => {
              setNotifications((prev) =>
                prev.map((n) => (n.id === item.id ? { ...n, read: true } : n))
              );
              notify.success(`Notification marked as read`);
            }}
            onOpen={(item) => notify(`Opened ${item.id}: ${item.title}`)}
          />
        </div>
      </ShowcaseCard>
    </div>
  );
}
