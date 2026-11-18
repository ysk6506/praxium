import { useState } from "react";
import {
  forgetToken,
  routeHash,
  saveTypedToken,
  titleOf,
  useEntryMode,
  useScreenTitle,
  useToken,
} from "../data";
import {
  Button,
  ButtonLink,
  Form,
  KeyValues,
  Mono,
  PageHeader,
  SecretField,
  Section,
  SubmitButton,
} from "../ui";

const MODE_NAMES = {
  local: "로컬 모드(접속 링크)",
  direct: "접근 토큰(직접 입력)",
  none: "접속하지 않음",
} as const;

/** The ways in, in the words of a person who has not used the product yet. */
function Ways() {
  return (
    <ul className="flex max-w-3xl flex-col gap-3 text-sm">
      <li>
        <strong className="font-semibold">로컬 모드</strong>
        <p className="text-muted">
          이 컴퓨터에서 혼자 쓰는 용도입니다. <Mono>praxium local -data-dir &lt;폴더&gt;</Mono>가
          터미널에 출력한 주소를 여십시오. 수집은{" "}
          <Mono>praxium ingest run -data-dir &lt;폴더&gt;</Mono>로 하고, 예제는{" "}
          <Mono>praxium ingest init</Mono>이 만듭니다. 토큰은 주소에 들어 있어 입력할 것이 없고,
          서버를 다시 시작하면 주소가 바뀝니다. 운영용이 아닙니다.
        </p>
      </li>
      <li>
        <strong className="font-semibold">개발 서버</strong>
        <p className="text-muted">
          개발 모드 서버(<Mono>api serve -mode development</Mono>)를 시작할 때{" "}
          <Mono>-dev-token-file</Mono>로 지정한 파일의 값을 아래에 입력합니다.
        </p>
      </li>
      <li>
        <strong className="font-semibold">운영 서버</strong>
        <p className="text-muted">
          관리자가 <Mono>praxium admin token create</Mono>로 만든 접근 토큰(<Mono>pxm_…</Mono>로
          시작합니다)을 아래에 입력합니다. 만든 직후에만 값이 보이므로 받은 사람이 바로 보관해야
          합니다.
        </p>
      </li>
    </ul>
  );
}

/** Where the person gives the access token. The browser keeps it and every request carries it. */
export function SettingsScreen() {
  const saved = useToken();
  useScreenTitle(titleOf("접속 설정"));
  const mode = useEntryMode();
  const [draft, setDraft] = useState("");
  const [problem, setProblem] = useState<string | undefined>(undefined);

  /** The form's submit: an empty box is told in words and gets focus (the button is never turned off for it). */
  function save(): boolean {
    if (draft.trim() === "") {
      setProblem("접근 토큰을 입력하십시오.");
      return false;
    }
    saveTypedToken(draft);
    setDraft("");
    setProblem(undefined);
    globalThis.location.hash = routeHash({ page: "kgs" });
    return true;
  }

  return (
    <>
      <PageHeader
        title="접속 설정"
        subtitle="이 화면이 읽는 API 서버에 접속하는 방법을 정합니다."
      />
      <Section title="접속하는 방법">
        <Ways />
      </Section>
      <Section title="접근 토큰 입력">
        <Form label="접근 토큰 입력" onSubmit={save} className="flex max-w-lg flex-col gap-3">
          <SecretField
            label="토큰"
            required
            value={draft}
            error={problem}
            onChange={(event) => {
              setDraft(event.target.value);
              setProblem(undefined);
            }}
            placeholder={
              saved ? "새 토큰으로 바꾸려면 입력하십시오" : "접근 토큰을 붙여 넣으십시오"
            }
            description="개발 서버는 -dev-token-file로 지정한 값, 운영 서버는 관리자가 만든 pxm_… 토큰입니다. 이 브라우저에 저장되며, 모든 API 요청의 Authorization 헤더에 실립니다."
          />
          <div className="flex gap-2">
            <SubmitButton>저장하고 접속</SubmitButton>
            {saved ? (
              <Button variant="ghost" onClick={() => forgetToken()}>
                저장된 토큰 삭제
              </Button>
            ) : null}
          </div>
        </Form>
      </Section>
      <Section title="현재 상태">
        <KeyValues
          items={[
            ["접속 방식", MODE_NAMES[mode]],
            ["토큰", saved ? "이 브라우저에 저장되어 있습니다" : "없습니다"],
            ["API 서버", globalThis.location.origin],
          ]}
        />
        {saved ? (
          <div className="mt-4">
            <ButtonLink href={routeHash({ page: "kgs" })}>KG 목록으로</ButtonLink>
          </div>
        ) : null}
      </Section>
    </>
  );
}
