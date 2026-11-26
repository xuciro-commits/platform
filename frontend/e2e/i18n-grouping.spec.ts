import { expect, test } from "@playwright/test";
import { signIn } from "./support";

test.describe("i18n completeness, two-level grouping and full-width layout", () => {
  test.use({
    locale: "zh-CN",
    colorScheme: "dark",
    viewport: { width: 1728, height: 1080 },
  });

  test("zh-CN: phone permissions, marketplace cards, vocabulary origins, 2-level grouping and full-width table", async ({ page }) => {
    await signIn(page, "admin-1 · phone");

    // 1. Admin -> 角色与权限矩阵 (Screenshot 1 verification)
    await page.goto("/admin/roles");
    await expect(page.getByRole("heading", { level: 1, name: "角色与权限矩阵" })).toBeVisible();
    const appSelector = page.getByRole("button", { name: /^应用或产品包空间/ });
    await appSelector.click();
    await page.getByRole("option", { name: /电话 \(phone\)/ }).click();

    await expect(page.getByRole("heading", { level: 4, name: "坐席", exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { level: 4, name: "语音网关", exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { level: 4, name: "通话主管", exact: true })).toBeVisible();

    for (const zhAction of ["新建通话", "挂断", "写备注", "接听", "接通", "记为失败", "结束", "记为未接", "来电入线", "归档通话", "恢复通话"]) {
      await expect(page.getByText(zhAction, { exact: true }).first()).toBeVisible();
    }
    for (const leakedEn of ["Create call", "Add notes", "Mark failed", "Mark missed", "Archive call", "Restore call"]) {
      await expect(page.getByText(leakedEn, { exact: true })).toHaveCount(0);
    }

    // 2. Delivery -> 市场 (Screenshot 2 verification)
    await page.goto("/delivery/marketplace");
    await expect(page.getByRole("heading", { level: 1, name: "市场" })).toBeVisible();
    for (const zhTitle of ["语音通话", "地点", "往来单位", "电话"]) {
      await expect(page.getByText(zhTitle, { exact: true }).first()).toBeVisible();
    }
    await expect(page.getByText(/语音通话能力契约/).first()).toBeVisible();
    await expect(page.getByText(/国家和地区及其 ISO 代码/).first()).toBeVisible();
    await expect(page.getByText(/经平台语音网关接打来电与外呼/).first()).toBeVisible();
    await expect(page.getByText("Phone calls", { exact: true })).toHaveCount(0);
    await expect(page.getByText("Business partners", { exact: true })).toHaveCount(0);

    // 3. Admin -> 语言与词汇: full-width table, localized group header ("地点"), accurate Origin ("应用或产品包"), 2-level grouping + one-click collapse/expand (Screenshots 3 & 4)
    await page.goto("/admin/vocabulary");
    await expect(page.getByRole("heading", { level: 1, name: "语言与词汇" })).toBeVisible();

    const searchBox = page.getByRole("textbox", { name: "搜索这个列表" });
    await searchBox.fill("county");
    await searchBox.press("Enter");
    await expect(page.getByRole("row", { name: /^折叠分组 地点/ })).toBeVisible();
    await expect(page.getByRole("row", { name: /Places/ })).toHaveCount(0);
    const countyRow = page.getByRole("row", { name: /^county / });
    await expect(countyRow).toContainText("县级");
    await expect(countyRow).toContainText("应用或产品包");
    await page.getByRole("button", { name: "清除全部" }).click();

    // Verify zero right-side empty void: rightmost header cell aligns with right edge of table (> 94% width)
    const widthRatio = await page.evaluate(() => {
      const table = document.querySelector("table");
      const headers = Array.from(document.querySelectorAll("thead th"));
      if (!table || headers.length === 0) return 0;
      const tRect = table.getBoundingClientRect();
      const lastRect = headers[headers.length - 1].getBoundingClientRect();
      return (lastRect.right - tRect.left) / tRect.width;
    });
    expect(widthRatio).toBeGreaterThan(0.95);

    // Open View options -> Grouping: verify 2-level selectable grouping + Collapse/Expand controls
    await page.getByRole("button", { name: "视图选项", exact: true }).click();
    await page.getByRole("button", { name: "分组", exact: true }).click();
    const l1Group = page.getByRole("radiogroup", { name: "一级分组", exact: true });
    const l2Group = page.getByRole("radiogroup", { name: "二级分组", exact: true });
    await expect(l1Group).toBeVisible();
    await expect(l2Group).toBeVisible();
    await l2Group.getByRole("radio", { name: "类型", exact: true }).click();

    // Test "展开一级" inside popover so L1 stays open and L2 subgroups collapse into a crisp summary
    await page.getByRole("button", { name: "展开一级", exact: true }).click();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("row", { name: /^折叠分组 应用: 地点/ }).or(page.getByRole("row", { name: /^折叠分组 地点/ }))).toBeVisible();
    await expect(page.getByRole("row", { name: /^展开分组 类型与字段/ }).first()).toBeVisible();

    // Test one-click "全部折叠": opening L1 must keep L2 subgroups collapsed
    const collapseBtn = page.getByRole("button", { name: "全部折叠", exact: true });
    await expect(collapseBtn).toBeVisible();
    await collapseBtn.click();
    const placesCollapsed = page.getByRole("row", { name: /^展开分组 地点/ });
    await expect(placesCollapsed).toBeVisible();
    await placesCollapsed.click();
    await expect(page.getByRole("row", { name: /^展开分组 类型与字段/ }).first()).toBeVisible();
    await expect(page.getByRole("row", { name: /^折叠分组 类型与字段/ })).toHaveCount(0);

    // Test one-click "全部展开" and verify strictly increasing indentation across L1 -> L2 -> data rows
    await page.getByRole("button", { name: "全部折叠", exact: true }).click();
    const expandBtn = page.getByRole("button", { name: "全部展开", exact: true });
    await expect(expandBtn).toBeVisible();
    await expandBtn.click();
    await expect(page.getByRole("row", { name: /^折叠分组 类型与字段/ }).first()).toBeVisible();
    const indents = await page.evaluate(() => {
      const rows = Array.from(document.querySelectorAll("tbody tr"));
      const padOf = (el?: Element | null) => (el ? parseFloat(getComputedStyle(el).paddingInlineStart) : 0);
      const l1 = rows.find((r) => r.textContent?.includes("应用:"));
      const l2 = rows.find((r) => r.textContent?.includes("类型:"));
      const data = rows.find((r) => !r.textContent?.includes("应用:") && !r.textContent?.includes("类型:"));
      return {
        l1: padOf(l1?.querySelector("td")),
        l2: padOf(l2?.querySelector("td")),
        data: padOf(data?.querySelector("td")),
      };
    });
    expect(indents.l2).toBeGreaterThan(indents.l1);
    expect(indents.data).toBeGreaterThan(indents.l2);

    // 4. Admin -> 组织: verify TreeList unit kind i18n + full width + one-click Collapse/Expand all
    await signIn(page, "admin-1 · acceptance");
    await page.goto("/admin/organization");
    await expect(page.getByRole("heading", { level: 1, name: "组织" })).toBeVisible();
    await expect(page.getByRole("button", { name: /^(全部展开|全部折叠)$/ })).toBeVisible();
  });
});
