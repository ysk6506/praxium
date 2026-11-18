---
title: ADR-0079 데모 모드를 없앤다 — `praxium demo`와 웹의 데모 표시를 지우고 첫 사용은 자기 파일의 `local`로 안내한다
kind: decision
status: active
enforced_by:
  - GOCOV001
  - TLOCK001
  - TRACE001
  - backend/cmd/praxium/local_test.go
  - backend/cmd/praxium/help_test.go
  - apps/web/src/data/entry.test.tsx
  - apps/web/src/data/access.test.tsx
  - apps/web/src/ui/banner.test.tsx
touches_protected:
  - AGENTS.md
  - docs/design-docs/adr/0038-demo-mode-as-a-supported-feature.md
  - docs/design-docs/adr/0041-cli-usability-local-mode-and-keeping-ingestion-results.md
  - docs/design-docs/adr/0042-web-ui-access-modes-url-state-errors-and-display-names.md
  - docs/design-docs/adr/0053-org-properties-command-line-and-start-wiring.md
  - docs/design-docs/adr/0054-impact-screen-wired-to-the-api.md
  - docs/design-docs/adr/0058-sources-from-db-and-api-worker-ingestion-and-editing-screens.md
  - docs/design-docs/adr/0062-kg-read-only-mcp-surface.md
  - docs/design-docs/adr/0068-kg-read-mcp-transport.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-definitions.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements-web-cli-release.md
  - docs/product-specs/requirements.md
  - harness/config/test-lock.json
updates_docs:
  - README.md
  - AGENTS.md
  - ARCHITECTURE.md
  - docs/design-docs/defaults.md
  - docs/design-docs/ingestion-service.md
  - docs/design-docs/kg-mcp.md
  - docs/design-docs/source-editing.md
  - docs/design-docs/temporary-console.md
  - docs/guides/connect-agents.md
  - docs/guides/ingest-definitions.md
  - docs/guides/operate-the-server.md
  - docs/guides/org-model.md
  - docs/guides/release.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-definitions.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements-web-cli-release.md
  - docs/product-specs/requirements.md
amends_tests:
  - apps/web/src/data/access.test.tsx::=setup
  - apps/web/src/data/screenstate.test.tsx::[REQ-297] a new screen opens at the top, and back returns to where the person was > keeps the position when only the query changed by a button, and goes to the top for an entry address
  - apps/web/src/impact-screen.test.tsx::[REQ-381] the words of the screen > asks for an access token on the connection screen and says where to get it
  - go:backend/cmd/praxium::TestTopLevelHelpListsEveryCommandInGroups
  - go:backend/cmd/praxium::TestEveryCommandAnswersHelpWithUsageFlagsAndAnExample
  - go:backend/cmd/praxium::TestAWrongCommandLinePrintsOneLineAboutItAndWhereTheHelpIs
  - go:backend/cmd/praxium::TestGlobalFlagsMayFollowTheCommand
  - go:backend/cmd/praxium::TestApiServeWithoutAModeSaysWhatToRunInsteadWithoutDebtNumbers
  - go:backend/cmd/praxium::TestTheExamplesGiveTheirServersAnIdentifierThatFindsThem
  - go:backend/cmd/praxium::TestTheTableFormShowsNestedValuesAndTheJSONFormIsUnchanged
  - go:backend/cmd/praxium::TestLocalServesTheKGOfAFolderWithoutMakingUpData
  - go:backend/cmd/praxium::TestLocalExplainsABuildWithoutTheWebUI
  - go:backend/cmd/praxium::TestIsLoopbackAddr
  - go:backend/cmd/praxium::TestOrgPropertiesGoFromTheFileThroughIngestionToQueriesAndSurviveARestart
  - apps/web/src/data/access.test.tsx::[REQ-296] an authentication error speaks in the words of the way in > sends a demo or local-mode visitor to the new address, and leaves a typed token as it was
  - "apps/web/src/data/access.test.tsx::[REQ-296] the page knows how the person came in: demo, local mode or a typed token > <suite>"
  - "apps/web/src/data/access.test.tsx::[REQ-296] the page knows how the person came in: demo, local mode or a typed token > marks a local-mode entry and lands on the KG list, and a later demo entry replaces the mark"
  - "apps/web/src/data/access.test.tsx::[REQ-296] the page knows how the person came in: demo, local mode or a typed token > reads the mode of an entry address"
  - "apps/web/src/data/access.test.tsx::[REQ-296] the page knows how the person came in: demo, local mode or a typed token > takes a new address pasted into the open page, replacing the address before the token is announced"
  - "apps/web/src/data/access.test.tsx::[REQ-296] the page knows how the person came in: demo, local mode or a typed token > tells the components the mode, and forgets it with the token or when another token is typed"
  - apps/web/src/data/access.test.tsx::<import:fb83b27e>
  - apps/web/src/data/entry.test.tsx::[REQ-281] the page takes the token of a demo address and shows that it is a demo > <suite>
  - apps/web/src/data/entry.test.tsx::[REQ-281] the page takes the token of a demo address and shows that it is a demo > is a demo only while the saved token is the one that came in by the demo address
  - apps/web/src/data/entry.test.tsx::[REQ-281] the page takes the token of a demo address and shows that it is a demo > keeps the mark in memory when the browser gives no storage
  - apps/web/src/data/entry.test.tsx::[REQ-281] the page takes the token of a demo address and shows that it is a demo > reads the token and the demo flag from the fragment, and nothing from any other address
  - apps/web/src/data/entry.test.tsx::[REQ-281] the page takes the token of a demo address and shows that it is a demo > saves the token, marks the demo and replaces the address with the KG screen
  - apps/web/src/data/entry.test.tsx::[REQ-281] the page takes the token of a demo address and shows that it is a demo > takes a token without the demo flag as no demo, and leaves other addresses alone
  - apps/web/src/data/entry.test.tsx::[REQ-281] the page takes the token of a demo address and shows that it is a demo > tells a component about the demo and stops when the token is replaced or deleted
  - apps/web/src/data/entry.test.tsx::<import:90ab63fc>
  - apps/web/src/ui/banner.test.tsx::[REQ-281] the frame shows a banner above everything when it is given one > <suite>
  - apps/web/src/ui/banner.test.tsx::[REQ-281] the frame shows a banner above everything when it is given one > puts the banner in a status element and leaves it out when there is none
  - go:backend/cmd/praxium::TestAllFourStartsRefuseDefinitionsThatCannotBeUsedAndSayHowToGoOn
  - go:backend/cmd/praxium::TestAStartThatTakesLongSaysItIsStillWorking
  - go:backend/cmd/praxium::TestDemoChecksTheFormOfTheAddressBeforeAnythingElse
  - go:backend/cmd/praxium::TestDemoFillsTheKGAndTheAPIAnswers
  - go:backend/cmd/praxium::TestDemoKeepsTheKGInADataFolder
  - go:backend/cmd/praxium::TestDemoRefusesAFolderWhoseFirstLoadDidNotFinish
  - go:backend/cmd/praxium::TestDemoRefusesBadArguments
  - go:backend/cmd/praxium::TestDemoSaysWhenThePortIsTaken
  - go:backend/cmd/praxium::TestDemoServesMCPWithTheTokenOfTheRun
  - go:backend/cmd/praxium::TestDemoStopsBeforeServingWhenTheWorldCannotBeMade
  - go:backend/cmd/praxium::TestDemoTokenIsNewEveryRunAndOnlyInTheAddress
  - go:backend/cmd/praxium::TestDemoURLNeedsAnAddressWithAPort
  - go:backend/cmd/praxium::TestDemoWarnsForANonLoopbackAddress
  - go:backend/cmd/praxium::TestDemoWithMCPOffAnswersJSON404
  - go:backend/cmd/praxium::TestDemoWithoutTheWebUIExplainsAndKeepsTheAPI
  - go:backend/cmd/praxium::TestIngestDataDirRefusesADemoFolderAndALockedOne
test_change_reason: 제품 책임자가 데모 모드는 제품에 두지 않기로 했다. 데모 명령과 데모 표시만 확인하던 테스트는 지우고, `local`에도 해당하는 성질(실행마다 새 토큰이 주소의
amends:
  - adr: "0041"
    what: 결정 6의 로컬 모드가 데모와 같은 조립을 쓴다는 부분과 결정 1의 첫 화면 도움말 맨 위의 가장 빠른 시작. REQ-289와 REQ-294를 그에 맞게 고친다.
  - adr: "0042"
    what: 결정 1의 접속 방식 셋을 둘로(데모 링크와 그 접속 표시를 뺌), 결정 3의 인증 오류 문구와 접속 설정 화면의 설명에서 데모를 뺌. REQ-296을 그에 맞게 고친다.
  - adr: "0053"
    what: 결정 3의 시작 배선이 `demo`를 포함한다는 부분과 `apply` 뒤에 다시 시작할 서버의 목록. REQ-369를 그에 맞게 고친다.
  - adr: "0054"
    what: 결정 7의 접속 설정 화면이 데모를 포함해 네 곳의 토큰 얻는 곳을 안내한다는 부분. REQ-381을 그에 맞게 고친다.
  - adr: "0058"
    what: 결정의 수집을 쓸 수 없는 서버와 폴더의 목록에서 데모 폴더. 서버 목록의 `demo`.
  - adr: "0062"
    what: 결정 3의 `demo`, `local`, 개발 모드가 기본으로 켜진다는 부분. REQ-502를 그에 맞게 고친다.
  - adr: "0068"
    what: 켜고 끄기의 `api serve`, `demo`, `local` 목록과, 적합성 확인이 데모 서버를 띄운다는 부분(개발 서버로 바뀐다).
decision_hash: b4ba28effdb77b30
---
# ADR-0079 데모 모드를 없앤다

## 맥락

제품 책임자가 정했다. **데모 모드는 제품에 두지 않는다.** [ADR-0038](0038-demo-mode-as-a-supported-feature.md)은 `praxium demo`를 정식 기능으로 두고 합성 데이터센터를 명령 하나로 띄웠다. 이 결정은 그것을 없앤다. 처음 쓰는 사람은 자기 파일로 KG를 만드는 길(`praxium ingest init`, `ingest run -data-dir`, `local`)로 간다. 이 길은 이미 있고 릴리스 확인 스크립트가 매번 돈다.

데모가 없어져도 남기는 것이 있다. `praxium local`과 `api serve`는 그대로다. 데모와 `local`이 같이 쓰던 서버 시작 조립(`session`, `serveSession`)과 실행마다 새로 만드는 토큰은 `local`의 것으로 남는다. 합성 세계 생성기(`worldgen`)와 적재기(`devload`)는 테스트와 측정의 도구라서 남고, 개발 서버(`api serve -mode development -dev-truth`)가 그것으로 합성 세계를 올릴 수 있다. 스토리지 종류(메모리, 파일)의 선택도 그대로다.

## 결정

1. **`praxium demo`를 지운다.** 명령, 플래그(`-scale`, `-seed`), 도움말 항목, 합성 세계를 만들어 적재하는 코드(`demoLoad`, `synthesize`)를 지운다. 오래 걸리는 시작에서 진행을 알리던 `reportWhile`은 데모의 적재에서만 불렸으므로 같이 지운다. 옛 명령을 치면 `unknown command "demo"`와 종료 코드 2를 받는다.
2. **서버 시작 조립은 `local`의 것으로 둔다.** `demo.go`는 `session.go`가 되어 `session`, `serveSession`, 주소 검사, 접속 주소, 안내만 남긴다. `connectURL`의 `mark` 인자는 부르는 곳이 `local` 하나라 지우고 `&mode=local`을 직접 붙인다. 실행용 토큰의 주체 이름은 `demo-user`에서 `local-user`가 된다.
3. **웹의 데모 표시를 지운다.** 접속 방식은 `local`과 직접 입력 둘이다. `&demo=1`은 더는 특별하지 않고 평범한 토큰 입장으로 읽힌다. 맨 위 안내 줄과 배지는 로컬 모드의 것만 남고, 접속 설정 화면은 데모 항목을 뺀다. 접속 링크는 `#/kgs`(KG 목록)로 간다.
4. **첫 화면 도움말의 맨 위를 바꾼다.** `Fastest start:  praxium ingest init, then ingest run and local` 한 줄이다. `demo` 줄은 뺀다.
5. **스크립트를 바꾼다.** 릴리스 확인은 데모 블록을 지우고 개발 서버를 합성 세계(`worldgen`, `-dev-truth`)로 띄워 `/`와 `/v1/kgs`를 확인한다. 실행 파일이 뜨고 화면과 API가 답하는지 보는 수준은 같다. `pnpm mcp:conformance`도 같은 개발 서버로 띄운다(토큰은 이 스크립트가 만들어 파일로 준다). 새 명령이나 새 플래그는 없다. `.github`는 바꾸지 않았다.
6. **요구사항.** REQ-280과 REQ-281을 지우고 번호는 다시 쓰지 않는다. 지킬 가치가 있는 성질은 `local`로 옮긴다.

   | 번호 | 옛 | 새 |
   |---|---|---|
   | REQ-280 | `praxium demo`는 합성 세계를 적재해 API와 웹 UI를 낸다 … | 삭제. 포트 검사, 루프백 경고, 웹 UI 없는 빌드의 안내, 종료 신호는 `local`이 이미 한다 |
   | REQ-281 | 데모의 토큰은 실행마다 새로 만들고 접속 주소의 프래그먼트에만 있다. 웹 UI는 화면 전에 토큰을 저장하고 주소를 첫 화면으로 바꾸며 표시는 그 토큰에 묶는다. 데모면 안내 줄과 배지를 보인다 | 삭제. 토큰의 성질은 REQ-294로, 웹의 성질은 REQ-296으로 옮긴다 |
   | REQ-294 | `local`은 … 데모와 같은 접속 방식(실행마다 새 토큰, `#/connect?token=…&mode=local`)이고 … 포트 검사와 루프백이 아닌 주소 경고는 데모와 같다 … 데모와 `local`의 시작은 오래 걸리면 몇 초마다 진행을 알린다 | 토큰은 실행마다 커널 엔트로피로 새로 만들고 토큰 파일을 디스크에 쓰지 않으며 접속 주소의 프래그먼트에만 있다. 주소 형식 오류를 가장 먼저 말하고, 사용 중인 포트는 적재 전에 안내하며, 루프백이 아닌 주소는 경고하고, 웹 UI 없는 빌드는 안내하고, 종료 신호에 서버를 닫는다. 진행 알림 문장은 삭제 |
   | REQ-296 | 접속 방식 셋(데모 링크, 로컬 모드 링크, 직접 입력) … 데모와 로컬 모드는 배너와 배지 … 접속 설정 화면은 세 방식을 설명 | 접속 방식 둘. 접속 링크의 토큰은 화면 전에 저장하고 주소를 KG 목록으로 바꾼다. 로컬 모드의 표시는 그 토큰에 묶인다. 설정 화면은 두 방식을 설명 |
   | REQ-289 | 맨 위에 가장 빠른 시작(`praxium demo`) | 맨 위에 가장 빠른 시작(`praxium ingest init`, `ingest run`, `local`) |
   | REQ-290 | … `praxium demo`, `praxium local`, `-mode development` … `demo`는 `-addr`의 형식 오류를 가장 먼저 말한다 | `praxium local`, `-mode development` … `local`은 `-addr`의 형식 오류를 가장 먼저 말한다 |
   | REQ-369, 381, 311, 443, 502, 493 | 문장에 `demo` 또는 데모 | 그 말만 뺀다(REQ-443의 "`demo`는 `viewer`다"는 문장째) |

   REQ-294의 진행 알림은 `local`이 폴더를 열 뿐 적재를 하지 않아 해당하는 일이 없어서 뺐다.
7. **ADR-0038을 대체한다.** 이 결정이 정한 것은 모두 위 1~3으로 지워진다. `status`를 `superseded`로, `superseded_by`를 이 ADR로 고친다(앞부분만 고치고 본문은 그대로다). 그러지 않으면 `enforced_by`가 가리키는 `demo_test.go`가 없어 `DOC080`을 어긴다.
8. **ADR-0041, 0042, 0053, 0054, 0058, 0062, 0068은 `amends`로 고친다.** 무엇이 바뀌는지는 앞부분에 적었다. 본문의 `demo` 언급은 그 시점의 기록이라 그대로 둔다.
9. **잠긴 테스트.** 지우거나 옮기는 키와 같은 키에서 바뀌는 항목 47개를 `amends_tests`에 적는다(Go 27, 웹 20과 지원 항목). 새로 잠그는 항목은 28개다. 단언을 줄인 테스트는 없다. 아래에 갈래별로 적는다.

   | 갈래 | Go | 웹 |
   |---|---|---|
   | 통째로 삭제(데모에만 있던 동작이거나 `local`에 같은 확인이 이미 있음) | 8 | 0 |
   | `local` 기준으로 다시 써서 새 이름으로 바꿈 | 8 | 17(이름이 바뀐 describe의 자식과 `<suite>`, `<import:…>`) |
   | 같은 키에서 내용이 바뀜 | 11 | 3(`=setup`, 입장 주소 입력 하나, REQ-381 낱말) |

   - 통째로 지우는 Go 테스트의 `local` 쪽 짝: 포트 사용 중, 잘못된 인자, 웹 UI 없는 빌드, 루프백이 아닌 주소는 `local_test.go`의 같은 이름 테스트가 이미 확인한다. 데모의 401과 루프백 경고 없음은 `TestLocalServesTheKGOfAFolderWithoutMakingUpData`에 단언으로 더했다.
   - 같은 키에서 바뀌는 11개 중 셋(`TestLocalExplainsABuildWithoutTheWebUI`, `TestTheExamplesGiveTheirServersAnIdentifierThatFindsThem`, `TestOrgPropertiesGoFromTheFileThroughIngestionToQueriesAndSurviveARestart`)은 본문이 같고, 도우미 `startLocal`이 선택 플래그를 받게 되어 지문만 바뀐다.
   - 테스트 도우미 `demoRun`, `demoGet`, `demoStop`, `demoCLI`, `tokenOf`의 이름은 그대로 둔다. 이름을 고치면 위 셋이 이름 때문에만 더 목록에 들어온다. 이름 정리는 뒤의 변경이다.
10. **커버리지 바닥은 낮추지 않는다.** 데모 코드를 지우자 `go:cmd.praxium` 측정이 하한 95.5 밑으로 내려가, 남은 코드에 뜻 있는 테스트 셋(옛 명령의 답, 스토어 오류, 읽을 수 없는 `-dev-truth`)을 더했다.

## 이유

- 데모는 합성 데이터로 제품을 보여 주는 길인데, 이 제품은 자기 데이터를 모아 읽는 것이 목적이다. 합성 세계를 첫 화면으로 두면 처음 쓰는 사람이 본 것과 자기 데이터가 어긋난다.
- 예제 파일과 안내서가 이미 저장소와 실행 파일에 있다(`ingest init`). 같은 일을 하는 길을 두 개 두면 문서, 테스트, 요구사항이 둘씩 늘어난다.
- 데모와 `local`이 같은 조립을 공유해서 데모 전용 코드는 많지 않고, 지우는 비용은 작다. 웹 UI의 접속 표시도 한 종류가 줄어 단순해진다.

## 대안과 기각 사유

- **데모를 두고 이름만 바꾼다.** 같은 일을 하는 길이 그대로 남는다.
- **예제 데이터를 넣어 주는 명령을 만든다.** 데모의 대체물이다. 새 명령과 그 테스트가 늘고 합성 세계를 첫 화면으로 두는 문제가 그대로다. 만들지 않는다. 예제는 `ingest init`이 쓴 파일을 읽는 것으로 충분하다.
- **ADR-0038을 `amends`로만 고친다.** 이 결정이 정한 것은 전부 지워지므로 일부만 고친다고 말할 수 없다. `DOC080`도 대체 없이는 어긴다.
- **테스트 도우미의 이름을 지금 고친다.** 단언이 같은 테스트 셋이 이름 때문에만 목록에 들어온다. 뒤의 변경으로 미룬다.

## 결과

- `praxium demo`가 없어진다. 웹 UI에는 데모 안내 줄과 배지가 없고, 로컬 모드와 직접 입력만 있다. 처음 쓰는 사람은 `praxium ingest init`부터 시작한다.
- 릴리스 노트용 한 줄: 이 버전부터 `praxium demo`가 없어지며, 처음에는 `praxium ingest init`으로 예제를 만들어 `ingest run`과 `local`로 열어 본다. `praxium local`의 서버에서 `kg_whoami`가 알려 주는 주체 이름이 `demo-user`에서 `local-user`로 바뀐다.
- 코드는 데모 전용 약 177줄(Go)과 약 60줄(웹)이 없어진다. 계약(REST)은 바뀌지 않는다.
- `ingest-demo`(계약의 예시 계정 이름)는 데모 모드와 무관하므로 그대로 둔다.
- 이 결정은 새 의존성도 새 설정도 더하지 않는다.

## 뒤의 변경

- 테스트 도우미 `demoRun`, `demoGet`, `demoStop`, `demoCLI`, `tokenOf`의 이름을 `local`에 맞게 고친다(그것을 쓰는 잠긴 테스트가 한꺼번에 바뀐다).
- 확정된 ADR 본문의 `demo` 언급은 그대로 둔다(그 시점의 기록).
