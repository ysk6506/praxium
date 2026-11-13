---
title: ADR-0027 KG를 API와 명령줄로 읽는다
kind: decision
status: active
enforced_by:
  - GOARCH010
  - GOARCH011
  - GOARCH021
  - GOARCH052
  - GOARCH053
  - GOTEST002
  - GOCOV001
  - TRACE006
  - API050
  - API051
  - API052
  - backend/internal/httpapi/kg_test.go
  - backend/internal/httpapi/conformance_test.go
  - backend/internal/cli/kg_test.go
  - backend/cmd/praxium/devstore_test.go
  - backend/internal/devload/load_test.go
  - backend/internal/readcheck/devload_test.go
  - backend/internal/graph/cardinality_test.go
  - backend/internal/graph/scratch_test.go
updates_docs:
  - ARCHITECTURE.md
  - AGENTS.md
  - docs/design-docs/kg-queries.md
  - docs/design-docs/kg-read-measurements.md
  - docs/design-docs/rest-api-resources-graph.md
  - docs/exec-plans/active/plan.md
  - docs/product-specs/requirements.md
touches_protected:
  - AGENTS.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
  - docs/product-specs/requirements.md
amends_tests:
  - go:backend/internal/httpapi::TestRefusalsOfTheQueriesKeepTheirCatalogueCodes
  - go:backend/internal/httpapi::TestQuestionsAnswerLikeTheirShortForm
  - go:backend/internal/httpapi::TestQuestionBodiesAreRefusedWithAProblem
  - go:backend/internal/cli::TestKGCommandsReportProblemsAndOddAnswers
  - go:backend/internal/cli::TestKGCommandsPrintWhatTheServerAnswered
  - go:backend/internal/cli::TestKGCommandsFollowCursorsAndRefuseWhatTheyCannotAsk
  - go:backend/internal/cli::TestKGCommandsAskTheRoutesTheyName
  - go:backend/internal/contract::TestEveryOperationDeclaresTheConventions
  - go:backend/internal/cli::TestExitCodesFollowTheProblemGroup
  - go:backend/internal/httpapi::TestAllowedActionsFollowTheRoleTable
  - go:backend/internal/httpapi::TestCatalogsServeTheClosedLists
  - go:backend/internal/httpapi::TestConditionalReadsAnswerNotModified
  - go:backend/internal/httpapi::TestCursorIsRefusedWhenItDoesNotBelong
  - go:backend/internal/httpapi::TestCursorPagingWalksEveryItemOnceInOrder
  - go:backend/internal/httpapi::TestEveryAnswerFitsTheContract
  - go:backend/internal/httpapi::TestEveryOperationAsksForTheRoleItDeclares
  - go:backend/internal/httpapi::TestFiltersSelectWhatTheModelHolds
  - go:backend/internal/httpapi::TestListQueryGrammarIsClosed
  - go:backend/internal/httpapi::TestMethodsAndPathsOutsideTheContractAreProblems
  - go:backend/internal/httpapi::TestObjectTypeDetailCarriesWhatWasAskedFor
  - go:backend/internal/httpapi::TestOldNamesAnswerAsAliases
  - go:backend/internal/httpapi::TestProblemsKeepTheirShapeAndCatalogue
  - go:backend/internal/httpapi::TestWithoutAnAuthenticatorOnlyTheOpenRoutesAnswer
  - go:backend/internal/query::TestFindExpandsSubtypesAndCountsByType
  - go:backend/internal/query::TestFindFiltersObjectsAndNeverMatchesOnAValueAboveTheCap
  - go:backend/internal/query::TestFindFiltersTableDriven
  - go:backend/internal/query::TestFindIsDeterministic
  - go:backend/internal/query::TestFindPagesAtASealedVersionKeepTheVersion
  - go:backend/internal/query::TestFindPagesRoundTripAndStayWhereTheyWereWhileTheKGMoves
  - go:backend/internal/query::TestFindRefusesWhatIsNotServedYet
  - go:backend/internal/query::TestFindSummaryHasCountsOnly
  - go:backend/internal/query::TestGetEnvelopeGolden
  - go:backend/internal/query::TestGetHonoursViewAndMaxStaleness
  - go:backend/internal/query::TestGetMasksSensitiveAttributesAndHidesObjects
  - go:backend/internal/query::TestGetReadsThePastWithoutVolatileValues
  - go:backend/internal/query::TestGetRefusesBadInputs
  - go:backend/internal/query::TestGetReturnsWhatTheIncludeListAsks
  - go:backend/internal/query::TestGrantsJudgeByLayerNamespaceAndScopeKeyConditions
  - go:backend/internal/query::TestNeighborsGroupsAndStepsAreReported
  - go:backend/internal/query::TestNeighborsLeaveHiddenObjectsOutAndCountThem
  - go:backend/internal/query::TestNeighborsPagesOfAHubRoundTrip
  - go:backend/internal/query::TestNeighborsReadThePastAndTheView
  - go:backend/internal/query::TestNeighborsRefusesBadInputs
  - go:backend/internal/query::TestNeighborsTableDriven
  - go:backend/internal/query::TestReadsWaitForMinSeqAndNoticeANewGeneration
test_change_reason: 길이 서른둘로 늘어 적합성 테스트가 계약의 길을 표에서 직접 도출하도록 네 테스트를 한 번 제대로 고치고(단언은 같거나 더 엄하다), 질의 테스트의 저장소를 핵심 단위 안의 `graph.ScratchStore`로 바꾸고 옛 자리를 읽는 다섯 테스트를 어댑터를 읽어도 되는 `adapter.readcheck`로 옮긴다. 나머지는 같은 이름의 도우미를 함께 쓰게 되어 지문만 바뀐 테스트이다. 계약의 경로 이름 테스트는 테스트의 경로 규칙이 ADR-0016의 `:action` 표기를 빠뜨려 설계 결정보다 좁았다. 결정에 맞게 고치고 POST 전용과 동사 조각 금지를 더했다.
decision_hash: ebb17d5971ee019a
---
# ADR-0027 KG를 API와 명령줄로 읽는다

## 맥락

ADR-0026이 첫 셋의 질의를 세웠고 3부와 4부를 이 변경에 미뤘다. 이 결정은 그 두 부분을 적는다: **3부** `/v1/kgs/{kg}` 아래의 길과 명령줄, **4부** 생성기의 정답을 개발용으로 KG에 싣는 적재기와 측정. 적재기는 개발과 테스트의 편의이지 수집 파이프라인(ADR-0008)이 아니다.

## 결정

1. **A. 적합성 테스트를 한 번 제대로 고친다.** 길이 늘면 테스트가 길마다 표를 더하는 일이 되풀이된다. 그래서 네 테스트가 연습할 길을 **길 표(서버가 낸 길)와 계약**에서 도출한다(`derivedOperations`). 계약에 있는데 표본(`sampleOf`: 경로 값, 질의 문자열, 몸)이 없는 길이 있으면 테스트가 실패한다. 길이 계약에도 표에도 없거나 둘이 다르면 `API050`~`API052`가 이미 막는다.
   - `TestEveryAnswerFitsTheContract`: 기존 교환은 그대로이고, 도출한 모든 길을 표본으로 불러 상태가 표본의 기대와 같고 응답이 계약에 맞는지 본다. 표본 없는 길은 실패다. 저장소 없는 서버의 `503` 교환 둘이 늘었다.
   - `TestProblemsKeepTheirShapeAndCatalogue`: 기존 단언은 그대로이고, 로그인이 필요한 모든 길을 로그인 없이 불러 `401`, 닫힌 코드, `application/problem+json`, 본문의 `trace_id`가 응답 머리와 같은지 본다.
   - `TestEveryOperationAsksForTheRoleItDeclares`: `GET`만이던 순회가 도출한 모든 길(`POST` 포함)을 표본 그대로 부르도록 바뀌었다. 익명, 수집자, 열람자의 기대는 같다.
   - `TestWithoutAnAuthenticatorOnlyTheOpenRoutesAnswer`: 같은 도출로 모든 길(`POST` 포함)을 부른다. 열린 길만 답하고 나머지는 `401`이라는 단언은 같다.
   - 이 변경에서 먼저 잠긴 테스트 일곱(`cli`의 `TestKGCommands...` 넷, `httpapi`의 `TestQuestionBodiesAreRefusedWithAProblem`, `TestQuestionsAnswerLikeTheirShortForm`, `TestRefusalsOfTheQueriesKeepTheirCatalogueCodes`)은 길의 글자만 `/questions/<이름>`에서 `/queries:<이름>`으로 바뀌었고 단언은 그대로다.
   - 나머지 `httpapi`와 `cli`의 테스트(`behaviour_test.go`, `TestExitCodesFollowTheProblemGroup`)은 단언이 그대로이고, 같은 이름의 도우미를 새 테스트 파일이 함께 쓰게 되어 지문만 바뀌었다.
2. **B. 핵심 단위는 어댑터를 읽지 않는다.** `adapter.http`는 `core.graph`와 `core.query`를 읽어도 되지만 운영 코드에서 `adapter.memstore`를 읽지 않는다. `core.query`도 테스트의 `memstore` 읽기를 거두었다. 저장소 포트의 최신만 쥔 구현 `graph.ScratchStore`가 `core.graph`에 있고(테스트 더블이자 개발의 쉬운 길이다), 질의 테스트는 그 위에서 돈다. 옛 변경 번호와 옛 버전으로 읽는 다섯 테스트(`TestFindPagesAtASealedVersionKeepTheVersion`, `TestFindPagesRoundTripAndStayWhereTheyWereWhileTheKGMoves`, `TestGetReadsThePastWithoutVolatileValues`, `TestNeighborsReadThePastAndTheView`, `TestReadsWaitForMinSeqAndNoticeANewGeneration`)은 내용 그대로 테스트 전용 단위 `adapter.readcheck`로 옮겼다. 질의 테스트 열일곱 개는 저장소만 바꿨고 단언은 그대로다.
3. **C. 질의 POST 길이 이 변경에 든다.** `POST /v1/kgs/{kg}/queries:find`, `queries:get`, `queries:neighbors`는 ADR-0026의 질의를 몸이 있는 요청으로 부른다.
4. **길.** 계약에 8개가 늘었고(`GET /v1/kgs`, `/v1/kgs/{kg}`, `/objects`, `/objects/{object_id}`, `/objects/{object_id}/links`와 POST 셋) 서버의 길은 24개에서 32개가 되었다. 계획 목록(`planned/routes.json`)에서 네 GET을 옮겼다(173 -> 169; POST 셋은 계획에 없었다). 알파 1에서 남은 계획은 변경, 검색, 영향 범위 등 나머지 질의의 길이다. 별명 `main`은 KG ID로 풀린다. 읽기 권한 필터링은 호출자의 역할을 부여로 옮겨 `query.Grants`로 만든다. 저장소가 없으면 `503`이다. 사전 정의 질의는 행동이라서 계약의 행동 표기(콜론)로 쓴다: `POST /v1/kgs/{kg}/queries:{이름}`. 모음 `queries`는 복수이고 이름은 콜론 뒤의 행동이라 `find`와 `get`을 설정에 올릴 필요가 없다(`questions/find` 꼴은 동사 조각이라 `API002`가 거절했고 규칙은 풀지 않았다). 계획 목록의 나머지 열 가지도 같은 꼴이다: `queries:containment`, `paths`, `impact`, `exposure`, `changes`, `versions`, `quality`, `health`, `competency`(아홉 POST)와 `GET /scopes`. 역량 질문의 자원(`/v1/competency-questions`)은 제 이름을 그대로 둔다.
5. **명령줄.** `kg list|get`, `object get|links`, `query find|get|neighbors`는 공개된 `/v1`만 부른다(`adapter.cli`는 `net/http`와 `embed`만 더 읽고 `os`는 읽지 않는다).
6. **배선.** `api serve -mode development`는 `cmd/praxium`이 메모리 저장소를 만들어 길에 꽂는다. `-dev-truth <파일>`이 정답을 적재한 KG(`dev`, 별명 `main`)를 낸다. 운영 모드는 진짜 인증기와 운영 저장소(PostgreSQL, 알파 1의 남은 몫)가 둘 다 없음을 이름으로 말하고 거부한다.
7. **적재기(`adapter.devload`).** 정답을 읽어 객체 키를 부모부터 풀고, 한 묶음에 정해진 수(기본 5,000)의 객체 다음에 링크를 적용기에 넣는다. 받을 수 없는 것(식별자 값이 규칙에 안 맞거나 키가 만들어지지 않는 객체, 끝이 없는 링크)은 세고 건너뛰어 `Report`에 낸다. 변경 배치의 나눔은 그래프 해시에 영향이 없고, 버전 ID는 묶음과 시계(원천 시각)에 의존한다. 같은 입력과 같은 시계의 되풀이는 같은 해시와 같은 버전 ID를 준다. `praxium dev load-truth`는 버려지는 저장소에 적재하고 수를 찍는다. 개발 모드 전용이다.
8. **핵심의 고침 셋.** (1) 차수(`1:1`, `N:1`)는 링크 타입이 아니라 링크가 놓인 **바인딩마다** 센다(컨테이너 클러스터의 통제부와 이름공간 때문에 드러났다; REQ-261). 모델 등록부에 `BindingFor`가 늘었다. (2) 큰 세계를 싣는 비용을 줄였다: 등록부 복제의 메모이제이션, 스칼라 값의 빠른 길, 메모리 저장소의 식별자 색인, 임대 기간 12시간(`lease_lost`를 막는다). 첫 측정은 3만 5천 객체에 24초와 3.0GB였다. (3) `ScratchStore`.
9. **측정.** `docs/design-docs/kg-read-measurements.md`에 기계 명세와 함께 적었다: 작은 세계(객체 522)와 큰 세계(객체 약 10만, 링크 약 18만)의 적재 시간, 메모리, 질의 시간. 부채 TD-0011이 큰 세계의 적재 시간과 `find`의 선형 훑기를 쥔다.
10. **구조 계약.** 단위 `adapter.devload`와 `adapter.readcheck`(테스트 전용)를 더하고, `core.query`의 `may_import`에서 `adapter.memstore`를 뺐으며, `adapter.http`가 `core.graph`와 `core.query`를 읽게 했다. 단위의 하한은 올린다.
11. **검사와 확정된 결정이 어긋나면 결정이 규칙이고 검사를 맞춘다.** 좁게, 결정이 말하는 것보다 느슨하지 않게. `TestEveryOperationDeclaresTheConventions`(`backend/internal/contract/contract_test.go`)의 경로 규칙이 ADR-0016과 `rest-api.md`의 `:action` 표기를 빠뜨려 결정보다 좁았다. 고친 것:
    - 옛 `^/v1(/[a-z0-9]+(-[a-z0-9]+)*|/\{[a-z_]+\})+$`
    - 새 `^/v1(/[a-z0-9]+(-[a-z0-9]+)*|/\{[a-z_]+\})+(:[a-z]+(-[a-z]+)*)?$`: 행동은 길 끝의 `:행동` 하나뿐이고 모음 조각이나 `{id}` 뒤에 온다.
    - 더한 조임(검사 함수 `pathProblem`과 새 테스트 `TestThePathRuleAcceptsActionsAndRefusesWhatTheDecisionRefuses`): 콜론이 든 길은 POST만, 콜론은 끝 말고는 어디에도 없다(정규식이 막는다), 조각 하나가 동사 그대로면 거절(`/questions/find` 꼴, 동사는 하네스 설정의 `action_verbs`와 `find`, `get`). 하네스 설정은 건드리지 않았다.

## 이유

- 길 표에서 테스트를 도출하면 길을 더할 때 테스트를 고치지 않고, 표본을 잊으면 테스트가 실패한다.
- 핵심이 어댑터를 읽지 않으면 저장소를 바꿔도 질의와 길이 그대로다.
- 정답을 적용기로 넣어야 적재가 수집의 규칙(모델 검증, 차수, 키)을 같이 테스트한다.

## 대안과 버린 이유

- 테스트마다 길 표를 손으로 늘리기: 이번에도 잊혔다.
- 질의 테스트가 `memstore`를 계속 읽기: 핵심이 어댑터를 읽는 예외가 남는다.
- 적재기가 저장소에 직접 쓰기: 적용기의 검증을 건너뛴다.

## 한계

- 적재는 큰 세계에서 16초, 상주 1.5GB쯤이다(측정 문서). 천만 객체 규모는 이 저장소로 못 한다.
- 적재기의 버전 ID는 시계에 의존한다.

## 요구사항 표의 변화

- **나눴다**: REQ-140에서 REQ-255(GET 길), REQ-256(POST 질의 길); REQ-147에서 REQ-257(명령줄), REQ-260(개발 서버); REQ-258(적재기), REQ-259(핵심은 어댑터를 읽지 않는다), REQ-261(바인딩별 차수)은 새로 세웠다. 남은 REQ-140과 REQ-147은 예정으로 있다.
