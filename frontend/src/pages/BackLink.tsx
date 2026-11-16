import { Icon } from "@astryxdesign/core/Icon";
import { HStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { rtlStyles } from "@astryxdesign/core/utils";
import { ArrowLeftIcon } from "@heroicons/react/24/outline";

export function BackLink({ href, label }: { href: string; label: string }) {
  return (
    <Link href={href} type="supporting" color="secondary">
      <HStack as="span" gap={1} vAlign="center">
        <Icon icon={ArrowLeftIcon} size="sm" xstyle={rtlStyles.mirror} />
        {label}
      </HStack>
    </Link>
  );
}
