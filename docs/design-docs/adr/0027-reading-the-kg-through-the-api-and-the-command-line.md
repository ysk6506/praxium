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
test_change_reason: 경로가 32개로 늘어 적합성 테스트가 계약의 경로를 표에서 직접 도출하도록 테스트 4개를 한 번 제대로 수정하고(단언은 같거나 더 엄하다), 질의 테스트의 스토리지를 코어 유닛 안의 `graph.ScratchStore`로 바꾸고 옛 버전을 읽는 테스트 5개를 어댑터를 import해도 되는 `adapter.readcheck`로 옮긴다. 나머지는 같은 이름의 헬퍼를 함께 사용하게 되어 해시만 바뀐 테스트이다. 계약의 경로 이름 테스트는 테스트의 경로 규칙이 ADR-0016의 `:액션` 표기를 누락해 설계 결정보다 좁았다. 결정에 맞게 수정하고 POST 전용과 동사 세그먼트 금지를 추가했다.
decision_hash: 53de10c2f2349e2e
amended_by:
  - adr: "0044"
    what: 운영 모드가 실제 인증기를 기다린다는 배선
---
# ADR-0027 KG를 API와 명령줄로 읽는다

## 맥락

ADR-0026이 첫 3종의 질의를 정했고 3부와 4부를 이 변경으로 미뤘다. 이 결정은 그 두 부분을 적는다: **3부** `/v1/kgs/{kg}` 아래의 경로와 명령줄, **4부** 생성기의 정답을 개발용으로 KG에 적재하는 적재기와 측정. 적재기는 개발과 테스트의 편의이지 수집 파이프라인(ADR-0008)이 아니다.

## 결정

1. **A. 적합성 테스트를 한 번 제대로 수정한다.** 경로가 늘면 경로마다 표에 행을 추가하는 일이 반복된다. 그래서 테스트 4개가 실행할 경로를 **경로 표(서버가 제공하는 경로)와 계약**에서 도출한다(`derivedOperations`). 계약에 있는데 샘플(`sampleOf`: 경로 값, 질의 문자열, 본문)이 없는 경로가 있으면 테스트가 실패한다. 경로가 계약에도 표에도 없거나 둘이 다르면 `API050`~`API052`가 이미 차단한다.
   - `TestEveryAnswerFitsTheContract`: 기존 교환은 그대로이고, 도출한 모든 경로를 샘플로 호출해 상태가 샘플의 기대와 같고 응답이 계약에 맞는지 확인한다. 샘플 없는 경로는 실패다. 스토리지 없는 서버의 `503` 교환 2개가 추가됐다.
   - `TestProblemsKeepTheirShapeAndCatalogue`: 기존 단언은 그대로이고, 로그인이 필요한 모든 경로를 로그인 없이 호출해 `401`, 고정된 코드, `application/problem+json`, 본문의 `trace_id`가 응답 헤더와 같은지 확인한다.
   - `TestEveryOperationAsksForTheRoleItDeclares`: `GET`만 순회하던 것을 도출한 모든 경로(`POST` 포함)를 샘플 그대로 호출하도록 바꿨다. 익명, 수집자, 열람자의 기대는 같다.
   - `TestWithoutAnAuthenticatorOnlyTheOpenRoutesAnswer`: 같은 도출로 모든 경로(`POST` 포함)를 호출한다. 열린 경로만 응답하고 나머지는 `401`이라는 단언은 같다.
   - 이 변경에서 먼저 잠긴 테스트 7개(`cli`의 `TestKGCommands...` 4개, `httpapi`의 `TestQuestionBodiesAreRefusedWithAProblem`, `TestQuestionsAnswerLikeTheirShortForm`, `TestRefusalsOfTheQueriesKeepTheirCatalogueCodes`)는 경로 문자열만 `/questions/<이름>`에서 `/queries:<이름>`으로 바뀌었고 단언은 그대로다.
   - 나머지 `httpapi`와 `cli`의 테스트(`behaviour_test.go`, `TestExitCodesFollowTheProblemGroup`)는 단언이 그대로이고, 같은 이름의 헬퍼를 새 테스트 파일이 함께 사용하게 되어 해시만 바뀌었다.
2. **B. 코어 유닛은 어댑터를 import하지 않는다.** `adapter.http`는 `core.graph`와 `core.query`를 import해도 되지만 운영 코드에서 `adapter.memstore`를 import하지 않는다. `core.query`도 테스트의 `memstore` import를 제거했다. 스토리지 포트의 구현 중 최신 상태만 보유하는 `graph.ScratchStore`가 `core.graph`에 있고(테스트 더블이자 개발용의 간편한 구현이다), 질의 테스트는 그 위에서 실행된다. 옛 변경 번호와 옛 버전으로 읽는 테스트 5개(`TestFindPagesAtASealedVersionKeepTheVersion`, `TestFindPagesRoundTripAndStayWhereTheyWereWhileTheKGMoves`, `TestGetReadsThePastWithoutVolatileValues`, `TestNeighborsReadThePastAndTheView`, `TestReadsWaitForMinSeqAndNoticeANewGeneration`)는 내용 그대로 테스트 전용 유닛 `adapter.readcheck`로 옮겼다. 질의 테스트 17개는 스토리지만 바꿨고 단언은 그대로다.
3. **C. 질의 POST 경로가 이 변경에 포함된다.** `POST /v1/kgs/{kg}/queries:find`, `queries:get`, `queries:neighbors`는 ADR-0026의 질의를 본문이 있는 요청으로 호출한다.
4. **경로.** 계약에 8개가 추가됐고(`GET /v1/kgs`, `/v1/kgs/{kg}`, `/objects`, `/objects/{object_id}`, `/objects/{object_id}/links`와 POST 3개) 서버의 경로는 24개에서 32개로 늘었다. 계획 목록(`planned/routes.json`)에서 GET 4개를 옮겼다(173 -> 169; POST 3개는 계획에 없었다). 알파 1에서 남은 계획은 변경, 검색, 영향 범위 등 나머지 질의의 경로다. 별칭 `main`은 KG ID로 해석된다. 읽기 권한 필터링은 호출자의 역할을 권한 부여로 변환해 `query.Grants`로 만든다. 스토리지가 없으면 `503`이다. 사전 정의 질의는 액션이라서 계약의 액션 표기(콜론)로 쓴다: `POST /v1/kgs/{kg}/queries:{이름}`. 컬렉션 `queries`는 복수형이고 이름은 콜론 뒤의 액션이라 `find`와 `get`을 설정에 등록할 필요가 없다(`questions/find` 형식은 동사 세그먼트라 `API002`가 거부했고 규칙을 완화하지 않았다). 계획 목록의 나머지 질의 10종도 같은 형식이다: `queries:containment`, `paths`, `impact`, `exposure`, `changes`, `versions`, `quality`, `health`, `competency`(POST 9개)와 `GET /scopes`. 역량 질문의 자원(`/v1/competency-questions`)은 자체 이름을 그대로 사용한다.
5. **명령줄.** `kg list|get`, `object get|links`, `query find|get|neighbors`는 공개된 `/v1`만 호출한다(`adapter.cli`는 `net/http`와 `embed`만 추가로 import하고 `os`는 import하지 않는다).
6. **배선.** `api serve -mode development`는 `cmd/praxium`이 메모리 스토리지를 만들어 경로에 연결한다. `-dev-truth <파일>`이 정답을 적재한 KG(`dev`, 별칭 `main`)를 제공한다. 운영 모드는 실제 인증기와 운영 스토리지(PostgreSQL, 알파 1의 남은 과제)가 둘 다 없음을 이름으로 밝히고 거부한다.
7. **적재기(`adapter.devload`).** 정답을 읽어 부모부터 순서대로 객체 키를 해석하고, 배치 하나에 정해진 수(기본 5,000)의 객체를 넣은 다음 링크를 적용기에 넣는다. 수집할 수 없는 것(식별자 값이 규칙에 맞지 않거나 키가 만들어지지 않는 객체, 끝점이 없는 링크)은 세어서 건너뛰고 `Report`로 반환한다. 변경 배치의 분할은 그래프 해시에 영향이 없고, 버전 ID는 배치와 시계(원천 시각)에 의존한다. 같은 입력과 같은 시계로 반복하면 같은 해시와 같은 버전 ID가 나온다. `praxium dev load-truth`는 일회용 스토리지에 적재하고 개수를 출력한다. 개발 모드 전용이다.
8. **코어 수정 3건.** (1) 카디널리티(`1:1`, `N:1`)는 링크 타입이 아니라 링크가 놓인 **바인딩별로** 계산한다(컨테이너 클러스터의 컨트롤 플레인과 네임스페이스 때문에 발견됐다; REQ-261). 모델 등록부에 `BindingFor`가 추가됐다. (2) 큰 세계의 적재 비용을 줄였다: 등록부 복제의 메모이제이션, 스칼라 값의 빠른 경로, 메모리 스토리지의 식별자 색인, 리스 기간 12시간(`lease_lost`를 방지한다). 첫 측정은 3만 5천 객체에 24초와 3.0GB였다. (3) `ScratchStore`.
9. **측정.** `docs/design-docs/kg-read-measurements.md`에 머신 사양과 함께 적었다: 작은 세계(객체 522)와 큰 세계(객체 약 10만, 링크 약 18만)의 적재 시간, 메모리, 질의 시간. 부채 TD-0011이 큰 세계의 적재 시간과 `find`의 선형 스캔을 추적한다.
10. **구조 계약.** 유닛 `adapter.devload`와 `adapter.readcheck`(테스트 전용)를 추가하고, `core.query`의 `may_import`에서 `adapter.memstore`를 제거했으며, `adapter.http`가 `core.graph`와 `core.query`를 import하게 했다. 유닛의 커버리지 하한은 올린다.
11. **검사와 확정된 결정이 어긋나면 결정이 규칙이고 검사를 맞춘다.** 좁게, 결정이 말하는 것보다 느슨하지 않게 한다. `TestEveryOperationDeclaresTheConventions`(`backend/internal/contract/contract_test.go`)의 경로 규칙이 ADR-0016과 `rest-api.md`의 `:액션` 표기를 누락해 결정보다 좁았다. 수정한 것:
    - 옛 `^/v1(/[a-z0-9]+(-[a-z0-9]+)*|/\{[a-z_]+\})+$`
    - 새 `^/v1(/[a-z0-9]+(-[a-z0-9]+)*|/\{[a-z_]+\})+(:[a-z]+(-[a-z]+)*)?$`: 액션은 경로 끝의 `:액션` 하나뿐이고 컬렉션 세그먼트나 `{id}` 뒤에 온다.
    - 추가한 제약(검사 함수 `pathProblem`과 새 테스트 `TestThePathRuleAcceptsActionsAndRefusesWhatTheDecisionRefuses`): 콜론이 든 경로는 POST만, 콜론은 끝 이외에는 어디에도 없다(정규식이 차단한다), 세그먼트 하나가 동사 그대로면 거부(`/questions/find` 형식, 동사는 하네스 설정의 `action_verbs`와 `find`, `get`). 하네스 설정은 수정하지 않았다.

## 이유

- 경로 표에서 테스트를 도출하면 경로를 추가할 때 테스트를 수정하지 않아도 되고, 샘플을 빠뜨리면 테스트가 실패한다.
- 코어가 어댑터를 import하지 않으면 스토리지를 바꿔도 질의와 경로가 그대로다.
- 정답을 적용기에 넣어야 적재가 수집의 규칙(모델 검증, 카디널리티, 키)을 함께 테스트한다.

## 대안과 버린 이유

- 테스트마다 경로 표를 수작업으로 늘리기: 이번에도 누락됐다.
- 질의 테스트가 `memstore`를 계속 import하기: 코어가 어댑터를 import하는 예외가 남는다.
- 적재기가 스토리지에 직접 쓰기: 적용기의 검증을 건너뛴다.

## 한계

- 적재는 큰 세계에서 16초, 상주 1.5GB쯤이다(측정 문서). 천만 객체 규모는 이 스토리지로 처리할 수 없다.
- 적재기의 버전 ID는 시계에 의존한다.

## 요구사항 표의 변화

- **분리했다**: REQ-140에서 REQ-255(GET 경로), REQ-256(POST 질의 경로); REQ-147에서 REQ-257(명령줄), REQ-260(개발 서버); REQ-258(적재기), REQ-259(코어는 어댑터를 import하지 않는다), REQ-261(바인딩별 카디널리티)은 새로 추가했다. 남은 REQ-140과 REQ-147은 예정으로 남아 있다.
