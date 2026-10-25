import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { Rejection, setLanguage } from "../index";

afterEach(cleanup);

test("a refusal shows its plain-language title, the next action, and hides the trail behind a disclosure", () => {
  setLanguage("en");
  render(<Rejection title="Access refused" message="You do not hold the required role." advice="Ask an administrator to review access before trying again." code="ERROR_CODE_POLICY_DENIED" requestKey="act-12" trail={<p>fact one</p>} />);
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect(screen.getByRole("alert", { name: "Access refused" })).toBeTruthy();
  expect(screen.getByText("Access refused")).toBeTruthy();
  expect(screen.getByText("You do not hold the required role.")).toBeTruthy();
  expect(screen.getByText("Ask an administrator to review access before trying again.")).toBeTruthy();
  expect(screen.queryByText("ERROR_CODE_POLICY_DENIED")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: /Technical details/ }));
  expect(screen.getByText("ERROR_CODE_POLICY_DENIED")).toBeTruthy();
  expect(screen.getByText("Request: act-12")).toBeTruthy();
  expect(screen.getByText("fact one")).toBeTruthy();
});

test("a waiting outcome reads as status, not as an error", () => {
  setLanguage("en");
  render(<Rejection waiting message="The answer was lost on the way." />);
  expect(screen.getByRole("status")).toBeTruthy();
  expect(screen.getByText("This request is still awaiting confirmation.")).toBeTruthy();
});
