import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Component, type ReactNode } from "react";
import { t } from "../i18n";

type Props = { children: ReactNode; compact?: boolean };
type State = { failed: boolean };

export class ErrorBoundary extends Component<Props, State> {
  state: State = { failed: false };

  static getDerivedStateFromError(): State {
    return { failed: true };
  }

  retry = () => {
    this.setState({ failed: false });
  };

  render() {
    if (!this.state.failed) return this.props.children;
    if (this.props.compact) {
      return (
        <VStack gap={2} hAlign="start">
          <Text type="supporting" color="secondary">{t("This section could not be displayed")}</Text>
          <Button label={t("Try again")} variant="secondary" size="sm" onClick={this.retry} />
        </VStack>
      );
    }
    return (
      <EmptyState
        headingLevel={2}
        title={t("Something went wrong")}
        description={t("This section could not be displayed")}
        actions={<Button label={t("Try again")} variant="secondary" onClick={this.retry} />}
      />
    );
  }
}
