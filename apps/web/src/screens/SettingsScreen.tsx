import { type FormEvent, useState } from "react";
import { forgetToken, routeHash, saveTypedToken, useEntryMode, useToken } from "../data";
import { Button, ButtonLink, KeyValues, Mono, PageHeader, Section, TextField } from "../ui";

const MODE_NAMES = {
  demo: "데모(접속 링크)",
  local: "로컬 모드(접속 링크)",
  direct: "개발용 토큰(직접 입력)",
  none: "접속하지 않음",
} as const;

/** The three ways in, in the words of a person who has not used the product yet. */
function Ways() {
  return (
    <ul className="flex max-w-3xl flex-col gap-3 text-sm">
      <li>
        <strong className="font-semibold">데모</strong>
        <p className="text-muted">
          <Mono>praxium demo</Mono>가 터미널에 출력한 주소를 브라우저로 여십시오. 토큰이 주소에 들어
          있어 입력할 것이 없습니다. 합성 데이터이며, 서버를 다시 시작하면 주소가 바뀝니다.
        </p>
      </li>
      <li>
        <strong className="font-semibold">로컬 모드</strong>
        <p className="text-muted">
          이 컴퓨터에서 혼자 쓰는 용도입니다. 서버가 터미널에 출력한 주소를 여십시오. 데모와 같이
          토큰은 주소에 들어 있고, 운영용이 아닙니다.
        </p>
      </li>
      <li>
        <strong className="font-semibold">개발용 토큰 직접 입력</strong>
        <p className="text-muted">
          개발 모드 서버(<Mono>api serve -mode development</Mono>)를 시작할 때{" "}
          <Mono>-dev-token-file</Mono>로 지정한 토큰을 아래에 입력합니다.
        </p>
      </li>
    </ul>
  );
}

/** Where the person gives the development token. The browser keeps it and every request carries it. */
export function SettingsScreen() {
  const saved = useToken();
  const mode = useEntryMode();
  const [draft, setDraft] = useState("");

  function save(event: FormEvent): void {
    event.preventDefault();
    if (draft.trim() === "") return;
    saveTypedToken(draft);
    setDraft("");
    globalThis.location.hash = routeHash({ page: "kgs" });
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
      <Section title="개발용 토큰 입력">
        <form onSubmit={save} className="flex max-w-lg flex-col gap-3">
          <TextField
            label="토큰"
            type="password"
            mono
            autoComplete="off"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={
              saved ? "새 토큰으로 바꾸려면 입력하십시오" : "서버를 시작할 때 지정한 토큰"
            }
            description="서버를 시작할 때 -dev-token-file로 지정한 값입니다. 이 브라우저에 저장되며, 모든 API 요청의 Authorization 헤더에 실립니다."
          />
          <div className="flex gap-2">
            <Button type="submit" variant="primary" disabled={draft.trim() === ""}>
              저장하고 접속
            </Button>
            {saved ? (
              <Button variant="ghost" onClick={() => forgetToken()}>
                저장된 토큰 삭제
              </Button>
            ) : null}
          </div>
        </form>
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
