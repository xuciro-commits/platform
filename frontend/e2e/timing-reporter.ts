import type { Reporter, Suite, TestCase, TestResult, TestStep } from "@playwright/test/reporter";
import { begin, note } from "../../scripts/timing.mjs";

export default class TimingReporter implements Reporter {
  private tests = new Map<TestResult, ReturnType<typeof begin>>();
  private browser = new Map<TestStep, ReturnType<typeof begin>>();

  onBegin(_config: unknown, suite: Suite) {
    note("Playwright 已收集 " + suite.allTests().length + " 项用例");
  }

  onTestBegin(test: TestCase, result: TestResult) {
    this.tests.set(result, begin(test.title));
  }

  onStepBegin(_test: TestCase, _result: TestResult, step: TestStep) {
    if (step.category === "fixture" && step.title === "browser") {
      this.browser.set(step, begin("浏览器实例启动"));
    }
  }

  onStepEnd(_test: TestCase, _result: TestResult, step: TestStep) {
    this.browser.get(step)?.(!step.error);
    this.browser.delete(step);
  }

  onTestEnd(_test: TestCase, result: TestResult) {
    this.tests.get(result)?.(result.status === "passed");
    this.tests.delete(result);
  }
}
