---
title: ADR-0078 영향 범위 계산을 삭제한다 — 서버의 impact 질의, REST 경로, CLI, 계약은 지우고 사용자가 질의로 직접 구한다
kind: decision
status: active
enforced_by:
  - GOCOV001
  - TLOCK001
  - TLOCK004
  - TRACE001
  - API030
  - backend/internal/kgread/service_test.go
touches_protected:
  - AGENTS.md
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
  - docs/design-docs/adr/0043-impact-query-propagation-redundancy-and-limits.md
  - docs/design-docs/adr/0057-storage-array-power-redundancy-in-the-synthetic-world.md
  - docs/design-docs/adr/0059-model-links-and-redundancy-loss.md
  - docs/design-docs/adr/0061-gateway-as-a-praxium-component-approval-first.md
  - docs/design-docs/adr/0062-kg-read-only-mcp-surface.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-gateway.md
  - docs/product-specs/requirements-impact.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements.md
  - harness/config/architecture.yaml
  - harness/config/coverage-floor.json
  - harness/config/test-lock.json
updates_docs:
  - AGENTS.md
  - ARCHITECTURE.md
  - README.md
  - docs/design-docs/access-control.md
  - docs/design-docs/competency-examples.md
  - docs/design-docs/competency-questions.md
  - docs/design-docs/data-observability.md
  - docs/design-docs/defaults.md
  - docs/design-docs/definitions-at-agent-scale.md
  - docs/design-docs/gateway-safety.md
  - docs/design-docs/gateway.md
  - docs/design-docs/glossary-general.md
  - docs/design-docs/glossary.md
  - docs/design-docs/ingestion-extensions.md
  - docs/design-docs/ingestion.md
  - docs/design-docs/kg-mcp.md
  - docs/design-docs/kg-model.md
  - docs/design-docs/kg-operations.md
  - docs/design-docs/kg-queries.md
  - docs/design-docs/kg-read-measurements.md
  - docs/design-docs/kg-storage.md
  - docs/design-docs/model-links.md
  - docs/design-docs/pipeline.md
  - docs/design-docs/postgres-operations.md
  - docs/design-docs/process-direction.md
  - docs/design-docs/product-design.md
  - docs/design-docs/query-engine-shape.md
  - docs/design-docs/rest-api-feature-routes.md
  - docs/design-docs/rest-api-resources-graph.md
  - docs/design-docs/rest-api-walkthroughs.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/self-observability.md
  - docs/design-docs/standard-model.md
  - docs/design-docs/worldgen.md
  - docs/exec-plans/active/plan.md
  - docs/guides/connect-agents.md
  - docs/product-specs/requirements-access.md
  - docs/product-specs/requirements-gateway.md
  - docs/product-specs/requirements-ingestion.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements.md
  - docs/references/owner-requirements-from-terrarium.md
amends_tests:
  - go:backend/cmd/praxium::TestImpactAnswersAreTheSameOnTheMemoryTheFileAndThePostgresStore
  - go:backend/internal/cli::TestImpactCommandAsksTheRouteAndPrintsSummaryGroupsAndPaths
  - go:backend/internal/cli::TestImpactCommandPrintsAnUpstreamAnswerWithItsSinglePointsAndHiddenSteps
  - go:backend/internal/cli::TestImpactCommandSaysWhenNothingChangesAndListsUnfilledChannels
  - go:backend/internal/cli::TestImpactCommandUpstreamTakesATargetAndRefusesAMixedOrEmptyQuestion
  - go:backend/internal/httpapi::TestImpactOverHTTPAnswersASiteFailureAndAnUpstreamQuestion
  - go:backend/internal/httpapi::TestImpactOverHTTPRefusesWhatItCannotAnswerWithAProblem
  - go:backend/internal/kgread::TestAskNeighborsAndImpact
  - go:backend/internal/kgread::TestInputProblems
  - go:backend/internal/kgread::TestWithoutAStoreEveryReadIsUnavailable
  - go:backend/internal/query::TestImpactACapacityGroupIsUnknownWhenAMemberFailsAndSaysSo
  - go:backend/internal/query::TestImpactAContainerThatFailsTakesWhatIsContainedInItAndLinkCutsAreAccepted
  - go:backend/internal/query::TestImpactADeadObjectFailsEverythingThatIsPartOfItAndAHardPartFailsTheWhole
  - go:backend/internal/query::TestImpactAGroupWithoutMinAliveIsOnlyDecidedWhenEveryValueAgrees
  - go:backend/internal/query::TestImpactAHostOfAPoolThatCanFailOverOnlyInterruptsItsVMs
  - go:backend/internal/query::TestImpactALinkOfUnknownGradeGivesUnknownInBothAnswers
  - go:backend/internal/query::TestImpactASoftRequirementOnlyDegradesWhileAHardOneBringsTheObjectDown
  - go:backend/internal/query::TestImpactAnAssumedLinkCarriesAFailureOnlyIntoTheAnswerWithAssumptions
  - go:backend/internal/query::TestImpactAnAssumedMinAliveIsBelievedOnlyWithAssumptions
  - go:backend/internal/query::TestImpactAnDependencyCycleEndsAndKeepsEveryObjectOnIt
  - go:backend/internal/query::TestImpactAnObservedLinkOlderThanMaxStalenessIsNotUsed
  - go:backend/internal/query::TestImpactBothPdusTakeTheDualFedServerDownAndTheDownSpreadsToHostAndVM
  - go:backend/internal/query::TestImpactCoolingGroupOfThreeFansNeedingTwo
  - go:backend/internal/query::TestImpactHidesWhatTheCallerMayNotReadButKeepsTheRoute
  - go:backend/internal/query::TestImpactItemsSayWhichChannelNoLinkSupplies
  - go:backend/internal/query::TestImpactKeepsAtMostMaxItemsAndSaysSo
  - go:backend/internal/query::TestImpactMlagPairLosesOneSwitchThenBoth
  - go:backend/internal/query::TestImpactOnePduLeavesTheDualFedServerExposedAndTakesTheSingleOneDown
  - go:backend/internal/query::TestImpactOrderOfTheSeedsDoesNotChangeTheAnswer
  - go:backend/internal/query::TestImpactPagesInTheOrderOfSeverityAndOtherQueriesCursorsAreRefused
  - go:backend/internal/query::TestImpactRefusesWhatItCannotAnswer
  - go:backend/internal/query::TestImpactStopsAtMaxHopsAndSaysTheAnswerIsNotComplete
  - go:backend/internal/query::TestImpactStopsWhenTheTimeBudgetIsUsedUp
  - go:backend/internal/query::TestImpactUpstreamListsWhatTheTargetDependsOnAndWhichOnesAreSinglePoints
  - go:backend/internal/query::TestImpactUpstreamPagesAndCapsTheDependenciesItTries
  - go:backend/internal/query::TestImpactUpstreamWithAnAssumedLinkNamesTheSinglePointOnlyWithAssumptions
  - go:backend/internal/readcheck::TestImpactADeadMemoryModuleDegradesTheServerAndADeadCPUTakesItDown
  - go:backend/internal/readcheck::TestImpactAMoreFailuresNeverBringBackAnObjectThatWasDown
  - go:backend/internal/readcheck::TestImpactAnArrayLosesRedundancyWithOnePduAndGoesDownWithBoth
  - go:backend/internal/readcheck::TestImpactBothRackPdusTakeTheServersDownAndItReachesHostsAndVMs
  - go:backend/internal/readcheck::TestImpactDownstreamAndUpstreamAgreeOnWhatOneFailureDoesToATarget
  - go:backend/internal/readcheck::TestImpactEndsOnAWholeSiteAndOnEveryPduAtOnce
  - go:backend/internal/readcheck::TestImpactEveryObjectThatGoesDownWithASeedNamesTheSeedAsItsSinglePoint
  - go:backend/internal/readcheck::TestImpactFailureAcrossAnAssumedLinkIsMarkedAndOnlyBelievedWithAssumptions
  - go:backend/internal/readcheck::TestImpactFanGroupOfAServerNeedsAllButOneFan
  - go:backend/internal/readcheck::TestImpactMlagPairOfARackLosesOneSwitchAndThenBoth
  - go:backend/internal/readcheck::TestImpactOneHostOfAClusterOnlyInterruptsItsVMs
  - go:backend/internal/readcheck::TestImpactOneRackPduExposesTheDualFedServersAndDownsTheSinglyFedToRSwitch
  - go:backend/internal/readcheck::TestImpactSaysWhenALimitCutTheAnswer
  - go:backend/internal/readcheck::TestImpactTheOrderOfTheSeedsDoesNotChangeTheAnswerOnAGeneratedWorld
test_change_reason: 영향 범위 계산(impact 질의)을 제품에서 삭제하기로 했다(제품 책임자의 결정). 그 질의만 테스트하던 Go 테스트 47개와 질의 이름이 든 테스트 이름 하나를 지우고, 질의 이름이 한 줄 섞여 있던 kgread의 테스트 셋은 그 줄만 빼고 나머지 단언은 그대로 둔다. 테스트하던 동작이 사라진 것이지 단언을 느슨하게 한 것이 아니다.
breaks_contract:
  - POST /v1/kgs/{kg}/queries:impact
contract_break_reason: 영향 범위 계산을 제품이 하지 않기로 한 제품 책임자의 결정에 따라 queries:impact 경로를 계약에서 삭제한다. 이 경로의 클라이언트는 웹 화면뿐이었고 그 화면은 ADR-0077로 먼저 삭제했다. 사용자는 남은 질의로 직접 구한다.
amends:
  - adr: "0010"
    what: 결정 1의 질의 목록(13종에서 `impact`를 뺀 12종), 결정 3의 영향 범위의 의미(채널, 링크 의미, 이중화 그룹의 3값 평가), 결정 4의 영향 범위를 프로젝션으로 응답한다는 부분, 결정 5의 영향 범위 응답 시간 숫자, 결정 8의 영향 범위 응답이 숨김 객체를 경유할 때의 규칙. REQ-141과 REQ-142가 삭제된다. 프로젝션과 `exposure`, 경로와 나머지 질의는 그대로다.
  - adr: "0059"
    what: 결정 6의 이중화 상실을 `impact` 질의가 계산한다는 부분과 `impact`가 찾지 못하는 경우의 서술, 결정 2의 0단계가 영향 범위의 답을 바꾸지 않는다는 문장. 표준 모델의 이중화 필드와 `exposure`는 이 결정에서 바꾸지 않는다.
  - adr: "0061"
    what: 결정 8의 영향 범위를 쓰는 조건은 완전성 선언이 있어야 통과한다는 항목, 결정 12의 승인 화면이 영향 요약을 보여 준다는 문장. 준비 응답과 승인 화면에는 조건 평가와 조건의 증거만 남는다. REQ-456과 REQ-458을 그에 맞게 고친다.
  - adr: "0062"
    what: 결정 2의 MCP 툴 여섯 개에서 `kg_impact`를 빼 다섯 개로 한다는 부분, 결정 8의 영향 범위의 큰 결과, 결정 10의 영향 범위 계산의 프로세스 전체 동시 상한. REQ-500, REQ-511, REQ-513을 그에 맞게 고친다.
decision_hash: fcbd1f4b7574a38b
---
# ADR-0078 영향 범위 계산을 삭제한다

## 맥락

제품 책임자가 정했다([ADR-0077](0077-remove-the-impact-screen-users-ask-by-query.md)과 같은 결정이다). **영향 범위 계산은 제품이 하지 않는다. 사용자가 질의를 써서 직접 한다. 제품은 그것이 가능하게 만든다.** ADR-0077은 웹 화면을 먼저 지웠고, 이 결정은 그 뒤의 서버 쪽을 지운다. 화면이 없어진 뒤 `queries:impact`를 부르는 클라이언트는 남아 있지 않다.

서버에는 영향 범위를 계산하는 질의가 있다. 풀이기(전파 규칙, 이중화 그룹의 3값 평가, 근거 등급, 업스트림, 한도)와 그것을 부르는 읽기 유스케이스, REST 경로, 명령줄 명령, 계약 조각, 요구사항(REQ-300번대와 REQ-425~430)이다([ADR-0043](0043-impact-query-propagation-redundancy-and-limits.md)).

## 결정

1. **서버의 `impact` 질의를 지운다.** 풀이기와 그 파일 전부, KG 읽기 유스케이스의 `Impact`와 입력 해석(`AskImpact`), REST 경로 `POST /v1/kgs/{kg}/queries:impact`, 명령줄의 `query impact`를 지운다. 사전 정의 질의는 13종에서 12종이 된다. `exposure`, `paths`, `neighbors`, `containment`와 프로젝션은 그대로다.
2. **계약을 고친다.** 계약 조각 둘(영향 범위의 스키마와 경로)을 지우고 `openapi.json`을 다시 합치고 웹의 생성 API 코드를 다시 만든다. 경로 하나가 사라지므로 호환성을 깨는 변경이다. `breaks_contract`에 `POST /v1/kgs/{kg}/queries:impact`를 적는다(`API030`).
3. **MCP 툴 `kg_impact`는 두지 않는다.** 아직 구현 전이므로 설계와 요구사항에서 뺀다. 툴은 다섯 개(`kg_whoami`, `kg_catalog`, `kg_find`, `kg_get`, `kg_neighbors`)다.
4. **게이트웨이의 영향 범위도 뺀다.** 준비 응답과 승인 화면에는 "조건 평가와 조건의 증거"만 남는다. 영향 범위를 쓰는 조건의 완전성 선언 규칙과 승인 화면의 영향 요약을 설계와 요구사항에서 뺀다.
5. **요구사항을 삭제한다.** REQ-141, 142, 300~306, 425~430, 511의 행을 지우고 그 번호는 다시 쓰지 않는다(요구사항 입구의 번호 문단에 적었다). 요구사항을 삭제하는 정해진 절차는 리포지토리에 없다. 그래서 행을 그것을 테스트하던 테스트와 같은 변경에서 지운다(TRACE001). 고치는 요구사항은 다음과 같다.
   - REQ-140: 사전 정의 질의의 개수(13종)를 적지 않는다.
   - REQ-162: 영향 범위가 통과하면 마스킹된 객체의 수로만 응답한다는 문장을 뺀다.
   - REQ-435: "모델, 계약, 영향 범위의 답은 바뀌지 않는다"를 "모델과 계약은 바뀌지 않는다"로 고친다.
   - REQ-437: 둘로 가른다. 링크 인자의 값을 소스가 주는 수집 쪽은 수집 요구사항으로 남기고, 영향 범위의 답을 다루는 쪽은 삭제한다.
   - REQ-456, REQ-458: 4항에 따른다.
   - REQ-500: 툴 여섯 개를 다섯 개로 한다.
   - REQ-513: 영향 범위 계산의 동시 상한을 뺀다.
   REQ-145(질의 엔진을 여러 인스턴스로 기동한다)는 영향 범위가 아니라 질의 엔진의 요구사항이라 남긴다. REQ-146도 남긴다.
6. **ADR-0043을 대체한다.** 이 결정이 정한 것은 모두 위 1로 지워진다. `status`를 `superseded`로, `superseded_by`를 이 ADR로 고친다(앞부분만 고치고 본문은 그대로다). `enforced_by`가 가리키던 테스트가 모두 사라지므로 이렇게 해야 `DOC080`을 어기지 않는다.
7. **ADR-0057은 대체하지 않는다.** 이 결정의 합성 세계(스토리지 어레이의 전원 이중화 그룹)는 그대로 유효하다. `enforced_by`가 가리키던 영향 범위 시나리오 테스트 파일만 사라지므로 그 항목을 앞부분에서 뺀다. 본문은 그대로다.
8. **ADR-0010, 0059, 0061, 0062는 `amends`로 고친다.** 무엇이 바뀌는지는 앞부분의 `amends`에 적었다.
9. **잠긴 테스트.** 지우는 테스트의 잠금 항목 48개(위 1의 Go 테스트 47개와 이름을 바꾼 `kgread`의 테스트 하나)와 고쳐서 남기는 항목 둘을 `amends_tests`에 적는다. 이름을 바꾼 `TestAskNeighbors`는 새 항목으로 잠근다. 지원 코드의 잠금 항목은 Go에 없다.
10. **합성 세계와 표준 모델의 계산용 필드는 이 결정에서 바꾸지 않는다.** 생성기가 만드는 이중화 그룹과 모델의 링크 인자는 그대로 두고, 그 정리는 뒤의 별도 단계에서 다룬다.
11. **구조 설정.** `readcheck`의 테스트가 쓰던 코어 접근 유닛, 계약 유닛, HTTP 어댑터는 이제 쓰지 않으므로 `architecture.yaml`의 `test_may_import`에서 뺀다. 허용을 줄이는 변경이다.

## 이유

- 제품이 대신 풀어 주는 계산은 사용자가 질의로 직접 구한다는 방향과 맞지 않고, 남겨 두면 읽기 질의와 모델의 설계가 이 계산에 맞춰진다.
- 이 질의의 클라이언트는 삭제한 웹 화면뿐이었다. 쓰는 곳이 없는 계산과 그 테스트와 요구사항이 남으면 부채가 된다.
- 게이트웨이와 MCP는 아직 구현 전이어서, 영향 범위를 설계에서 지우는 비용이 작다.

## 대안과 기각 사유

- **서버에 `impact`를 두고 화면만 지운다(ADR-0077에서 끝낸다).** 쓰는 곳이 없는 계산과 한도, 동시 상한이 남는다.
- **`impact`를 숨기기만 한다(경로를 닫고 코드는 둔다).** 테스트와 요구사항이 부채로 남는다.
- **ADR-0043을 대체하지 않고 `amends`로만 고친다.** 이 결정이 정한 것은 전부 지워지므로 일부만 고친다고 말할 수 없다. `DOC080`도 대체 없이는 어긴다.
- **ADR-0057을 함께 대체한다.** 합성 세계의 결정은 영향 범위와 무관하게 유효하다.
- **`impact_store_test`의 대체 테스트를 더한다.** 지운 테스트가 확인하던 것은 지운 질의의 저장소 연결이다. 남은 질의가 같은 연결을 쓰는 곳은 다른 테스트가 이미 확인하고, 커버리지 바닥도 지켜진다.

## 결과

- 서버에서 영향 범위 계산이 사라지고, 계약에서 경로 하나(`queries:impact`)가 사라진다. 릴리스 노트용 한 줄: 이 버전부터 `POST /v1/kgs/{kg}/queries:impact`와 `praxium query impact`가 없어지며, 영향 범위는 `neighbors`, `paths`, `exposure` 같은 남은 질의로 직접 구한다.
- 요구사항 문서 `requirements-impact.md`가 없어지고 설계 문서 `kg-queries-examples.md`가 없어진다(영향 범위의 예시만 있었다).
- 커버리지 바닥은 낮추지 않는다. 지운 뒤의 유닛의 값은 바닥 이상이다.
- 영향 범위 테스트가 빠진 자리를 남은 코드의 테스트로 채우면서 `pnpm ratchet`이 커버리지 바닥을 올렸다(내린 것 없음).
- 이 결정은 새 의존성도 새 설정도 더하지 않는다.

## 뒤의 변경

표준 모델과 생성기의 계산용 필드(링크 인자의 `strength`, `whole_needs_part`, 이중화 그룹의 임계, 근거 등급의 가정 규칙)와 그것을 설명하는 설계 문서는 별도 단계에서 다룬다.
