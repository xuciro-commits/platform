import { useMediaQuery } from "@astryxdesign/core/hooks";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Heading, Text } from "@astryxdesign/core/Text";
import type { ReactNode } from "react";

export type Gutter = 4 | 6;

export function useGutter(): Gutter {
  return useMediaQuery("(max-width: 768px)") ? 4 : 6;
}

export function PageTitle({ title, back, actions, children }: { title: string; back?: { href: string; label: string }; actions?: ReactNode; children?: ReactNode }) {
  return (
    <VStack gap={2}>
      <HStack gap={3} vAlign="center" wrap="wrap" minHeight={32}>
        <StackItem size="fill">
          <HStack gap={2} vAlign="center">
            {back && (
              <>
                <Link href={back.href} type="large" size="lg" weight="semibold" color="secondary" maxLines={1}>{back.label}</Link>
                <Text type="large" size="lg" weight="semibold" color="secondary" aria-hidden="true">/</Text>
              </>
            )}
            <Heading level={3} accessibilityLevel={1} maxLines={2}>{title}</Heading>
          </HStack>
        </StackItem>
        {actions}
      </HStack>
      {children}
    </VStack>
  );
}
