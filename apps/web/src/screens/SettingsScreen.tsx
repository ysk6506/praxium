import { type FormEvent, useState } from "react";
import { connection, routeHash, useToken } from "../data";
import { Button, ButtonLink, KeyValues, PageHeader, Section, TextField } from "../ui";

/** Where the person gives the development token. The browser keeps it and every request carries it. */
export function SettingsScreen() {
  const saved = useToken();
  const [draft, setDraft] = useState("");

  function save(event: FormEvent): void {
    event.preventDefault();
    if (draft.trim() === "") return;
    connection.setToken(draft);
    setDraft("");
    globalThis.location.hash = routeHash({ page: "kgs" });
  }

  return (
    <>
      <PageHeader
        title="접속 설정"
        subtitle="이 화면이 읽는 API 서버에 접속하는 방법을 정합니다."
      />
      <Section title="개발용 토큰">
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
              <Button variant="ghost" onClick={() => connection.clear()}>
                저장된 토큰 삭제
              </Button>
            ) : null}
          </div>
        </form>
      </Section>
      <Section title="현재 상태">
        <KeyValues
          items={[
            ["토큰", saved ? "이 브라우저에 저장되어 있습니다" : "없습니다"],
            ["API 서버", globalThis.location.origin],
            ["인증 방식", "개발용 토큰(개발 모드 서버만 받습니다)"],
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
