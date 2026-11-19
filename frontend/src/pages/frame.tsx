import { useMediaQuery } from "@astryxdesign/core/hooks";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Heading } from "@astryxdesign/core/Text";
import type { ReactNode } from "react";
import { BackLink } from "./BackLink";

export type Gutter = 4 | 6;

export function useGutter(): Gutter {
  return useMediaQuery("(max-width: 768px)") ? 4 : 6;
}

export function PageTitle({ title, back, actions, children }: { title: string; back?: { href: string; label: string }; actions?: ReactNode; children?: ReactNode }) {
  return (
    <VStack gap={2}>
      {back && <BackLink href={back.href} label={back.label} />}
      <HStack gap={3} vAlign="center" wrap="wrap" minHeight={36}>
        <StackItem size="fill">
          <Heading level={1} maxLines={2}>{title}</Heading>
        </StackItem>
        {actions}
      </HStack>
      {children}
    </VStack>
  );
}
