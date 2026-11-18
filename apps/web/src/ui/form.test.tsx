// @vitest-environment happy-dom
import { act, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { click, render, settle } from "../test-support/render";
import { Button } from "./Button";
import { ConfirmDialog } from "./ConfirmDialog";
import {
  Form,
  ListEditor,
  NumberInput,
  type Result,
  ResultNotice,
  SecretField,
  SubmitButton,
  TextAreaField,
  TextField,
} from "./index";

afterEach(() => {
  document.body.innerHTML = "";
});

const input = (root: ParentNode, label: string): HTMLInputElement => {
  const found = [...root.querySelectorAll("label")].find((l) => l.textContent?.startsWith(label));
  const id = found?.getAttribute("for");
  const control = id ? root.querySelector(`#${CSS.escape(id)}`) : null;
  if (!control) throw new Error(`no input labelled ${label}`);
  return control as HTMLInputElement;
};

/** Type into an input the way React hears it. */
async function type(element: HTMLInputElement | HTMLTextAreaElement, text: string): Promise<void> {
  await act(async () => {
    const proto = element instanceof HTMLTextAreaElement ? HTMLTextAreaElement : HTMLInputElement;
    Object.getOwnPropertyDescriptor(proto.prototype, "value")?.set?.call(element, text);
    element.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function key(
  target: Element,
  init: KeyboardEventInit & { keyCode?: number },
): { event: KeyboardEvent } {
  const event = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...init });
  if (init.keyCode !== undefined) Object.defineProperty(event, "keyCode", { value: init.keyCode });
  act(() => {
    target.dispatchEvent(event);
  });
  return { event };
}

const focus = (element: HTMLElement): void => {
  act(() => element.focus());
};

const describedBy = (element: Element): string[] =>
  (element.getAttribute("aria-describedby") ?? "").split(" ").filter(Boolean);

describe("[REQ-409] a field ties its label, its help and its error to the input", () => {
  it("names the input by the label and describes it by the help", async () => {
    const view = await render(<TextField label="이름" description="영문 소문자로 씁니다" />);
    const control = input(view.container, "이름");
    expect(control.getAttribute("aria-invalid")).not.toBe("true");
    const help = describedBy(control).map((id) => document.getElementById(id)?.textContent);
    expect(help).toEqual(["영문 소문자로 씁니다"]);
    await view.unmount();
  });

  it("marks the input invalid and describes it by the error, which is read out as an alert", async () => {
    const view = await render(
      <TextField label="이름" description="도움말" error="필수 항목입니다" />,
    );
    const control = input(view.container, "이름");
    expect(control.getAttribute("aria-invalid")).toBe("true");
    const described = describedBy(control).map((id) => document.getElementById(id));
    expect(described.map((e) => e?.textContent)).toEqual(
      expect.arrayContaining(["도움말", "오류필수 항목입니다"]),
    );
    expect(view.container.querySelector('[role="alert"]')?.textContent).toContain(
      "필수 항목입니다",
    );
    await view.rerender(<TextField label="이름" description="도움말" />);
    expect(control.getAttribute("aria-invalid")).not.toBe("true");
    expect(view.container.querySelector('[role="alert"]')).toBeNull();
    await view.unmount();
  });

  it("shows several errors at once and says in words that the field is required", async () => {
    const view = await render(
      <TextField label="주소" required error={["형식이 올바르지 않습니다", "너무 깁니다"]} />,
    );
    const control = input(view.container, "주소");
    expect(control.required).toBe(true);
    expect(view.container.querySelector("label")?.textContent).toContain("(필수)");
    expect(view.container.querySelector('[role="alert"]')?.textContent).toContain("너무 깁니다");
    expect(describedBy(control).length).toBeGreaterThan(0);
    await view.unmount();
  });

  it("does the same for a text area and a number", async () => {
    const view = await render(
      <>
        <TextAreaField label="설명" description="여러 줄" error="비어 있습니다" rows={3} />
        <NumberInput
          label="포트"
          description="1 이상"
          error="범위를 벗어났습니다"
          value={null}
          onChange={() => undefined}
        />
      </>,
    );
    const area = input(view.container, "설명");
    expect(area.tagName).toBe("TEXTAREA");
    expect(area.getAttribute("rows")).toBe("3");
    expect(area.getAttribute("aria-invalid")).toBe("true");
    expect(describedBy(area)).toHaveLength(2);
    const number = input(view.container, "포트");
    expect(number.getAttribute("aria-invalid")).toBe("true");
    expect(describedBy(number)).toHaveLength(2);
    await view.unmount();
  });
});

describe("[REQ-409] a number is a number or nothing", () => {
  it("gives null for an empty box, the number for digits, and writes no thousands separator", async () => {
    const heard: (number | null)[] = [];
    function Port() {
      const [value, setValue] = useState<number | null>(8080);
      return (
        <NumberInput
          label="포트"
          value={value}
          onChange={(next) => {
            heard.push(next);
            setValue(next);
          }}
        />
      );
    }
    const view = await render(<Port />);
    const box = input(view.container, "포트");
    expect(box.value).toBe("8080");
    await type(box, "");
    await act(async () => box.dispatchEvent(new FocusEvent("blur", { bubbles: true })));
    expect(heard.at(-1)).toBeNull();
    await type(box, "1234");
    expect(heard.at(-1)).toBe(1234);
    await view.unmount();
  });
});

describe("[REQ-409] a secret is typed and kept nowhere by the field", () => {
  it("hides the characters, turns filling and checking off, and has no name", async () => {
    const view = await render(<SecretField label="토큰" value="" onChange={() => undefined} />);
    const box = input(view.container, "토큰");
    expect(box.type).toBe("password");
    expect(box.getAttribute("autocomplete")).toBe("off");
    expect(box.getAttribute("spellcheck")).toBe("false");
    expect(box.getAttribute("autocapitalize")).toBe("off");
    expect(box.hasAttribute("name")).toBe(false);
    await view.unmount();
  });

  it("leaves the address, the storage and the cookies as they were while the value is typed", async () => {
    const secret = "pxm_secret-value-0001";
    localStorage.clear();
    sessionStorage.clear();
    const before = {
      href: location.href,
      local: localStorage.length,
      session: sessionStorage.length,
    };
    function Holder() {
      const [value, setValue] = useState("");
      return <SecretField label="토큰" value={value} onChange={(e) => setValue(e.target.value)} />;
    }
    const view = await render(<Holder />);
    await type(input(view.container, "토큰"), secret);
    expect(input(view.container, "토큰").value).toBe(secret);
    expect({
      href: location.href,
      local: localStorage.length,
      session: sessionStorage.length,
    }).toEqual(before);
    expect(document.cookie).not.toContain(secret);
    expect(location.href).not.toContain(secret);
    await view.unmount();
  });
});

describe("[REQ-409] a list is edited from the keyboard", () => {
  type Row = { name: string };
  function Names(props: { start: Row[]; addOnEnter?: boolean; errors?: (string | undefined)[] }) {
    const [items, setItems] = useState<readonly Row[]>(props.start);
    return (
      <ListEditor
        label="헤더"
        itemName="헤더"
        items={items}
        onChange={setItems}
        newItem={() => ({ name: "" })}
        addOnEnter={props.addOnEnter}
        itemErrors={props.errors}
        renderItem={(item, index) => (
          <TextField
            label={`이름 ${index + 1}`}
            value={item.name}
            onChange={(event) =>
              setItems(items.map((it, i) => (i === index ? { name: event.target.value } : it)))
            }
          />
        )}
      />
    );
  }
  const names = (root: ParentNode): string[] =>
    [...root.querySelectorAll("input")].map((i) => (i as HTMLInputElement).value);
  const button = (root: ParentNode, name: string): HTMLButtonElement => {
    const found = [...root.querySelectorAll("button")].find(
      (b) => b.getAttribute("aria-label") === name || b.textContent === name,
    );
    if (!found) throw new Error(`no button ${name}`);
    return found as HTMLButtonElement;
  };

  it("adds, removes and moves items with buttons that say which item they are for", async () => {
    const view = await render(<Names start={[{ name: "a" }, { name: "b" }, { name: "c" }]} />);
    await click(button(view.container, "헤더 더하기"));
    expect(names(view.container)).toEqual(["a", "b", "c", ""]);
    await click(button(view.container, "헤더 2 지우기"));
    expect(names(view.container)).toEqual(["a", "c", ""]);
    await click(button(view.container, "헤더 2 위로 옮기기"));
    expect(names(view.container)).toEqual(["c", "a", ""]);
    await click(button(view.container, "헤더 1 아래로 옮기기"));
    expect(names(view.container)).toEqual(["a", "c", ""]);
    expect(view.container.querySelector("legend")?.textContent).toBe("헤더");
    await view.unmount();
  });

  it("keeps focus on the item that was moved, and keeps the ends focusable", async () => {
    const view = await render(<Names start={[{ name: "a" }, { name: "b" }]} />);
    const up = button(view.container, "헤더 2 위로 옮기기");
    focus(up);
    await click(up);
    expect(names(view.container)).toEqual(["b", "a"]);
    // The button went with its item: it is now the first item's, so it cannot go further, yet it keeps focus.
    expect(document.activeElement).toBe(up);
    expect(up.getAttribute("aria-label")).toBe("헤더 1 위로 옮기기");
    expect(up.hasAttribute("disabled")).toBe(false);
    expect(up.getAttribute("aria-disabled")).toBe("true");
    await click(up);
    expect(names(view.container)).toEqual(["b", "a"]);
    await view.unmount();
  });

  it("moves an item with Alt and the arrow keys in its input", async () => {
    const view = await render(<Names start={[{ name: "a" }, { name: "b" }, { name: "c" }]} />);
    const second = input(view.container, "이름 2");
    key(second, { key: "ArrowUp", altKey: true });
    expect(names(view.container)).toEqual(["b", "a", "c"]);
    key(input(view.container, "이름 2"), { key: "ArrowDown", altKey: true });
    expect(names(view.container)).toEqual(["b", "c", "a"]);
    const { event } = key(input(view.container, "이름 1"), { key: "ArrowUp" });
    expect(event.defaultPrevented).toBe(false);
    await view.unmount();
  });

  it("puts focus into the new item after adding, and on a neighbour after removing", async () => {
    const view = await render(<Names start={[{ name: "a" }, { name: "b" }]} />);
    await click(button(view.container, "헤더 더하기"));
    await settle();
    expect(document.activeElement).toBe(input(view.container, "이름 3"));
    await click(button(view.container, "헤더 3 지우기"));
    await settle();
    expect(document.activeElement).toBe(input(view.container, "이름 2"));
    await click(button(view.container, "헤더 2 지우기"));
    await click(button(view.container, "헤더 1 지우기"));
    await settle();
    expect(document.activeElement).toBe(button(view.container, "헤더 더하기"));
    expect(view.container.textContent).toContain("항목이 없습니다.");
    await view.unmount();
  });

  it("adds an item on Enter when asked to, but not while a letter is being composed", async () => {
    const view = await render(<Names start={[{ name: "a" }]} addOnEnter />);
    const first = input(view.container, "이름 1");
    const composing = key(first, { key: "Enter", isComposing: true });
    const safari = key(first, { key: "Enter", keyCode: 229 });
    expect(composing.event.defaultPrevented).toBe(false);
    expect(safari.event.defaultPrevented).toBe(false);
    expect(names(view.container)).toEqual(["a"]);
    key(first, { key: "Enter" });
    expect(names(view.container)).toEqual(["a", ""]);
    await view.unmount();
  });

  it("does not add an item on Enter unless asked to", async () => {
    const view = await render(<Names start={[{ name: "a" }]} />);
    key(input(view.container, "이름 1"), { key: "Enter" });
    expect(names(view.container)).toEqual(["a"]);
    await view.unmount();
  });

  it("says what is wrong with an item, next to it, and ties it to the item", async () => {
    const view = await render(
      <Names start={[{ name: "a" }, { name: "" }]} errors={[undefined, "비어 있습니다"]} />,
    );
    const alerts = [...view.container.querySelectorAll('[role="alert"]')];
    expect(alerts.map((a) => a.textContent)).toEqual(["오류비어 있습니다"]);
    const group = [...view.container.querySelectorAll("fieldset")].find(
      (f) => f.querySelector("legend")?.textContent === "헤더 2",
    );
    expect(group?.getAttribute("aria-describedby")).toBe(alerts[0]?.id);
    await view.unmount();
  });
});

describe("[REQ-409] a form sends on submit and sends focus to what to fix", () => {
  function Sample(props: { onSubmit: () => boolean | Promise<boolean>; formError?: boolean }) {
    const [result, setResult] = useState<Result | undefined>(undefined);
    return (
      <Form
        label="소스 저장"
        onSubmit={async () => {
          const ok = await props.onSubmit();
          setResult(
            ok
              ? { tone: "success", title: "저장했습니다" }
              : { tone: "error", title: "저장하지 못했습니다" },
          );
          return ok;
        }}
      >
        <TextField label="이름" error={props.formError ? undefined : "필수 항목입니다"} />
        <TextField label="주소" error={props.formError ? undefined : "형식이 올바르지 않습니다"} />
        <SubmitButton>저장</SubmitButton>
        <ResultNotice result={result} />
      </Form>
    );
  }
  const submit = (root: ParentNode): HTMLFormElement =>
    root.querySelector("form") as HTMLFormElement;
  const send = async (root: ParentNode): Promise<void> => {
    await act(async () => {
      submit(root).dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();
  };

  it("moves focus to the first invalid field when the submit fails", async () => {
    const view = await render(<Sample onSubmit={() => false} />);
    await send(view.container);
    expect(document.activeElement).toBe(input(view.container, "이름"));
    await view.unmount();
  });

  it("moves focus to the failure notice when no field is to blame", async () => {
    const view = await render(<Sample onSubmit={() => false} formError />);
    await send(view.container);
    expect(document.activeElement?.hasAttribute("data-form-result")).toBe(true);
    await view.unmount();
  });

  it("leaves focus alone when the submit succeeds, and waits for an answer that comes later", async () => {
    let release: ((ok: boolean) => void) | undefined;
    const view = await render(
      <Sample
        onSubmit={() =>
          new Promise<boolean>((resolve) => {
            release = resolve;
          })
        }
      />,
    );
    focus(input(view.container, "주소"));
    await send(view.container);
    await act(async () => release?.(true));
    expect(document.activeElement).toBe(input(view.container, "주소"));
    await view.unmount();
  });

  it("does not turn the submit button off before the submit, and turns it off only while it is going on", async () => {
    const view = await render(
      <Form label="f" onSubmit={() => true}>
        <SubmitButton>저장</SubmitButton>
      </Form>,
    );
    const sending = view.container.querySelector("button") as HTMLButtonElement;
    expect(sending.hasAttribute("disabled")).toBe(false);
    expect(sending.getAttribute("type")).toBe("submit");
    await view.rerender(
      <Form label="f" onSubmit={() => true}>
        <SubmitButton pending>저장</SubmitButton>
      </Form>,
    );
    expect(view.container.textContent).toContain("저장하는 중");
    expect(sending.getAttribute("aria-disabled") === "true" || sending.disabled).toBe(true);
    await view.unmount();
  });

  it("does not let Enter that confirms a letter send the form", async () => {
    const view = await render(
      <Form label="f" onSubmit={() => true}>
        <TextField label="이름" />
      </Form>,
    );
    const box = input(view.container, "이름");
    expect(key(box, { key: "Enter", isComposing: true }).event.defaultPrevented).toBe(true);
    expect(key(box, { key: "Enter", keyCode: 229 }).event.defaultPrevented).toBe(true);
    expect(key(box, { key: "Enter" }).event.defaultPrevented).toBe(false);
    expect(key(box, { key: "a", isComposing: true }).event.defaultPrevented).toBe(false);
    await view.unmount();
  });

  it("does not take Enter away from a text area", async () => {
    const view = await render(
      <Form label="f" onSubmit={() => true}>
        <TextAreaField label="설명" />
      </Form>,
    );
    const area = input(view.container, "설명");
    expect(key(area, { key: "Enter", isComposing: true }).event.defaultPrevented).toBe(false);
    await view.unmount();
  });
});

describe("[REQ-409] the result of a save is read out", () => {
  it("has both places in the page before anything is written into them", async () => {
    const view = await render(<ResultNotice result={undefined} />);
    expect(view.container.querySelector("output")).not.toBeNull();
    expect(view.container.querySelector('[role="alert"]')).not.toBeNull();
    expect(view.container.textContent).toBe("");
    const output = view.container.querySelector("output");
    const alert = view.container.querySelector('[role="alert"]');
    await view.rerender(<ResultNotice result={{ tone: "success", title: "저장했습니다" }} />);
    expect(view.container.querySelector("output")).toBe(output);
    expect(output?.textContent).toContain("저장했습니다");
    expect(alert?.textContent).toBe("");
    await view.rerender(
      <ResultNotice
        result={{
          tone: "error",
          title: "저장하지 못했습니다",
          message: "서버가 거절했습니다",
          hint: "다시 시도하십시오",
        }}
      />,
    );
    expect(view.container.querySelector('[role="alert"]')).toBe(alert);
    expect(alert?.textContent).toContain("서버가 거절했습니다");
    expect(alert?.textContent).toContain("오류");
    expect(output?.textContent).toBe("");
    await view.unmount();
  });
});

describe("[REQ-409] a dialog that asks keeps focus in and gives it back", () => {
  function Asking(props: { onConfirm: () => void; danger?: boolean; pending?: boolean }) {
    const [open, setOpen] = useState(false);
    return (
      <>
        <Button onClick={() => setOpen(true)}>지우기</Button>
        <ConfirmDialog
          open={open}
          onOpenChange={setOpen}
          onConfirm={props.onConfirm}
          title="소스를 지울까요?"
          description="이 소스의 매핑도 함께 사라집니다."
          confirmLabel="지우기"
          danger={props.danger}
          pending={props.pending}
        />
      </>
    );
  }
  const dialog = (): HTMLElement | null => document.querySelector('[role="alertdialog"]');
  const inDialog = (name: string): HTMLButtonElement =>
    [...(dialog()?.querySelectorAll("button") ?? [])].find(
      (b) => b.textContent === name,
    ) as HTMLButtonElement;

  it("opens as an alert dialog with the title and starts focus on cancel", async () => {
    const view = await render(<Asking onConfirm={() => undefined} />);
    const opener = view.container.querySelector("button") as HTMLButtonElement;
    focus(opener);
    await click(opener);
    await settle();
    expect(dialog()?.textContent).toContain("소스를 지울까요?");
    expect(dialog()?.getAttribute("aria-labelledby")).toBeTruthy();
    expect(document.activeElement).toBe(inDialog("취소"));
    await view.unmount();
  });

  it("closes on cancel and on Esc, and gives focus back to the button that opened it", async () => {
    const view = await render(<Asking onConfirm={() => undefined} />);
    const opener = view.container.querySelector("button") as HTMLButtonElement;
    focus(opener);
    await click(opener);
    await settle();
    await click(inDialog("취소"));
    await settle();
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(opener);
    await click(opener);
    await settle();
    key(document.activeElement as Element, { key: "Escape" });
    await settle();
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(opener);
    await view.unmount();
  });

  it("calls onConfirm on the confirm button and stays open until the caller closes it", async () => {
    const onConfirm = vi.fn();
    const view = await render(<Asking onConfirm={onConfirm} />);
    await click(view.container.querySelector("button") as Element);
    await settle();
    await click(inDialog("지우기"));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(dialog()).not.toBeNull();
    await view.unmount();
  });

  it("says in words that a dangerous action cannot be taken back, and waits while it is going on", async () => {
    const view = await render(<Asking onConfirm={() => undefined} danger />);
    await click(view.container.querySelector("button") as Element);
    await settle();
    expect(dialog()?.textContent).toContain("되돌릴 수 없습니다");
    await view.rerender(<Asking onConfirm={() => undefined} danger pending />);
    await settle();
    const wait = [...(dialog()?.querySelectorAll("button") ?? [])].map((b) => b.textContent);
    expect(wait).toContain("처리하는 중");
    await view.unmount();
  });

  it("does not say so for an action that can be taken back", async () => {
    const view = await render(<Asking onConfirm={() => undefined} />);
    await click(view.container.querySelector("button") as Element);
    await settle();
    expect(dialog()?.textContent).not.toContain("되돌릴 수 없습니다");
    await view.unmount();
  });
});
