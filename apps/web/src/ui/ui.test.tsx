// @vitest-environment happy-dom
import { act } from "react";
import { describe, expect, it, vi } from "vitest";
import { click, render, settle } from "../test-support/render";
import { cx } from "./cx";
import {
  AppShell,
  Badge,
  Button,
  Checkbox,
  Dialog,
  EmptyState,
  ErrorState,
  KeyValues,
  LoadingState,
  Mono,
  NavLink,
  NavSection,
  PageHeader,
  Pagination,
  Section,
  Select,
  Table,
  Tabs,
  TBody,
  Td,
  TextField,
  THead,
  Th,
  Tr,
} from "./index";

describe("[REQ-278] buttons, badges and fields", () => {
  it("calls back on a click and not when disabled", async () => {
    const onClick = vi.fn();
    const view = await render(
      <>
        <Button onClick={onClick}>저장</Button>
        <Button variant="primary" disabled onClick={onClick}>
          막힘
        </Button>
        <Button variant="ghost">조용히</Button>
      </>,
    );
    const buttons = view.container.querySelectorAll("button");
    await click(buttons[0] as Element);
    await click(buttons[1] as Element);
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(buttons[0]?.getAttribute("type")).toBe("button");
    await view.unmount();
  });

  it("shows a badge in each tone", async () => {
    const view = await render(
      <>
        {(["neutral", "ok", "warn", "danger", "accent"] as const).map((tone) => (
          <Badge key={tone} tone={tone}>
            {tone}
          </Badge>
        ))}
        <Badge>기본</Badge>
      </>,
    );
    expect(view.container.querySelectorAll("span")).toHaveLength(6);
    await view.unmount();
  });

  it("ties the label and the description to the input", async () => {
    const view = await render(
      <TextField label="토큰" description="서버를 띄울 때 정한 값" type="password" mono />,
    );
    const input = view.container.querySelector("input") as HTMLInputElement;
    const label = view.container.querySelector("label");
    expect(label?.textContent).toBe("토큰");
    expect(input.type).toBe("password");
    expect(input.getAttribute("aria-describedby")).toBeTruthy();
    await view.unmount();
    const plain = await render(<TextField label="이름" />);
    expect(plain.container.textContent).toBe("이름");
    await plain.unmount();
  });

  it("joins class names and skips what is not set", () => {
    expect(cx("a", false, null, undefined, "", "b")).toBe("a b");
  });
});

describe("[REQ-278] states say what is going on", () => {
  it("announces loading with a label", async () => {
    const view = await render(<LoadingState />);
    expect(view.container.querySelector("output")?.textContent).toBe("불러오는 중");
    await view.rerender(<LoadingState label="객체를 읽는 중" />);
    expect(view.container.textContent).toBe("객체를 읽는 중");
    await view.unmount();
  });

  it("shows an empty state with its explanation and action", async () => {
    const view = await render(
      <EmptyState title="객체가 없습니다" action={<Button>돌아가기</Button>}>
        이 타입의 객체가 아직 없습니다.
      </EmptyState>,
    );
    expect(view.container.textContent).toContain("객체가 없습니다");
    expect(view.container.querySelector("button")).not.toBeNull();
    await view.rerender(<EmptyState title="비었음" />);
    expect(view.container.querySelector("button")).toBeNull();
    await view.unmount();
  });

  it("shows an error as an alert with the hint and the facts for a report", async () => {
    const view = await render(
      <ErrorState
        title="토큰이 맞지 않습니다"
        message="서버가 거절했습니다."
        hint="다시 입력하세요."
        details={[
          ["코드", "auth.token_invalid"],
          ["추적 ID", "tr-1"],
        ]}
        action={<Button>설정으로</Button>}
      />,
    );
    expect(view.container.querySelector('[role="alert"]')).not.toBeNull();
    expect(view.container.textContent).toContain("auth.token_invalid");
    expect(view.container.textContent).toContain("다시 입력하세요.");
    await view.rerender(<ErrorState title="오류" message="m" details={[]} />);
    expect(view.container.querySelector("dl")).toBeNull();
    await view.unmount();
  });
});

describe("[REQ-278] a table, the layout and the lists of facts", () => {
  it("writes a table with column headers", async () => {
    const view = await render(
      <Table aria-label="객체">
        <THead>
          <Tr>
            <Th>이름</Th>
          </Tr>
        </THead>
        <TBody>
          <Tr>
            <Td>R1</Td>
          </Tr>
        </TBody>
      </Table>,
    );
    expect(view.container.querySelector("th")?.getAttribute("scope")).toBe("col");
    expect(view.container.querySelector("td")?.textContent).toBe("R1");
    await view.unmount();
  });

  it("marks the current page in the navigation and gives the page a trail", async () => {
    const view = await render(
      <AppShell
        brand="프락시움"
        nav={
          <NavSection title="탐색">
            <NavLink href="#/kgs" current>
              KG
            </NavLink>
            <NavLink href="#/settings">설정</NavLink>
          </NavSection>
        }
        footer="v0"
      >
        <PageHeader
          title="객체"
          subtitle="3개"
          trail={[{ label: "KG", href: "#/kgs" }, { label: "현재" }]}
          actions={<Button>새로고침</Button>}
        />
        <PageHeader title="단순" />
        <Section title="속성" aside="2개">
          <KeyValues
            items={[
              ["이름", "R1"],
              ["ID", <Mono key="id">obj_1</Mono>],
            ]}
          />
        </Section>
      </AppShell>,
    );
    const links = view.container.querySelectorAll("nav a");
    expect(links[0]?.getAttribute("aria-current")).toBe("page");
    expect(links[1]?.getAttribute("aria-current")).toBeNull();
    expect(view.container.querySelector('nav[aria-label="현재 위치"]')?.textContent).toContain(
      "현재",
    );
    expect(view.container.querySelectorAll("dt")).toHaveLength(2);
    await view.unmount();
  });
});

describe("[REQ-278] checkboxes, tabs, selects, dialogs and paging work from the keyboard and the pointer", () => {
  it("toggles a checkbox from its label", async () => {
    const onChange = vi.fn();
    const view = await render(
      <Checkbox checked={false} onChange={onChange}>
        모델
      </Checkbox>,
    );
    await click(view.container.querySelector("label") as Element);
    expect(onChange).toHaveBeenCalledWith(true);
    await view.unmount();
  });

  it("opens the tab that was clicked and shows only its content", async () => {
    const onChange = vi.fn();
    const items = [
      { value: "a" as const, label: "가", content: <p>내용 가</p> },
      { value: "b" as const, label: "나", content: <p>내용 나</p> },
    ];
    const view = await render(<Tabs value="a" onChange={onChange} items={items} />);
    const tabs = view.container.querySelectorAll('[role="tab"]');
    expect(tabs).toHaveLength(2);
    expect(tabs[0]?.getAttribute("aria-selected")).toBe("true");
    expect(view.container.textContent).toContain("내용 가");
    await act(async () => {
      (tabs[1] as HTMLElement).focus();
      (tabs[1] as HTMLElement).dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
      (tabs[1] as HTMLElement).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(onChange).toHaveBeenCalledWith("b");
    await view.unmount();
  });

  it("shows the chosen option of a select", async () => {
    const view = await render(
      <Select
        label="페이지당 개수"
        value={50}
        options={[
          { value: 25, label: "25개씩" },
          { value: 50, label: "50개씩" },
        ]}
        onChange={() => undefined}
      />,
    );
    const trigger = view.container.querySelector('[aria-label="페이지당 개수"]');
    expect(trigger?.textContent).toContain("50개씩");
    await view.unmount();
  });

  it("shows a dialog with its title while open and nothing when closed, and tells when it should close", async () => {
    const onOpenChange = vi.fn();
    const view = await render(
      <Dialog open title="컬럼" description="보일 속성" onOpenChange={onOpenChange}>
        본문
      </Dialog>,
    );
    await settle();
    expect(document.body.textContent).toContain("컬럼");
    expect(document.body.textContent).toContain("보일 속성");
    const close = [...document.querySelectorAll("button")].find((b) => b.textContent === "닫기");
    await click(close as Element);
    expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything());
    await view.rerender(
      <Dialog open={false} title="컬럼" onOpenChange={onOpenChange}>
        본문
      </Dialog>,
    );
    await settle();
    expect(document.body.textContent).not.toContain("본문");
    await view.unmount();
  });

  it("pages forward and back and changes the page size", async () => {
    const onPrevious = vi.fn();
    const onNext = vi.fn();
    const view = await render(
      <Pagination
        page={2}
        hasPrevious
        hasNext={false}
        onPrevious={onPrevious}
        onNext={onNext}
        pageSize={25}
        pageSizes={[25, 50]}
        onPageSize={() => undefined}
        summary="26–40 / 40"
      />,
    );
    const [previous, next] = [...view.container.querySelectorAll("button")].filter((b) =>
      ["이전", "다음"].includes(b.textContent ?? ""),
    );
    await click(previous as Element);
    await click(next as Element);
    expect(onPrevious).toHaveBeenCalledTimes(1);
    expect(onNext).not.toHaveBeenCalled();
    expect(view.container.textContent).toContain("26–40 / 40");
    expect(view.container.textContent).toContain("2페이지");
    await view.unmount();
  });
});
