---
title: ADR-0028 감사에서 나온 검사의 허점을 보완한다
kind: decision
status: active
enforced_by:
  - GOARCH005
  - GOARCH006
  - GOARCH010
  - GOARCH021
  - GOARCH054
  - GOARCH055
  - GOARCH056
  - GOTEST004
  - GOTEST005
  - GOTEST006
  - COV006
  - GOCOV003
  - TLOCK001
  - harness/src/checks/go-audit.test.ts
  - harness/src/checks/go-test-rules.test.ts
  - harness/src/checks/coverage-lag.test.ts
  - harness/src/checks/doc-stamps.test.ts
  - backend/internal/graphcheck/graphcheck_test.go
updates_docs:
  - AGENTS.md
  - ARCHITECTURE.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/adr-process.md
  - docs/product-specs/requirements.md
  - docs/exec-plans/tech-debt.yaml
touches_protected:
  - .github/workflows/ci.yml
  - AGENTS.md
  - harness/**
  - docs/product-specs/requirements.md
  - docs/design-docs/adr/0019-harness-reads-go-backend-place-and-checks.md
  - docs/design-docs/adr/0023-harness-locks-go-tests-and-links-them-to-requirements.md
  - docs/design-docs/adr/0024-rest-contract-becomes-a-document-the-machine-keeps.md
  - docs/design-docs/adr/0025-kg-stands-on-a-sequence-of-changes-storage-port-and-reference-implementation.md
amends_tests:
  - go:backend/internal/graphcheck::TestReferenceStorePassesTheSuite
  - harness/src/test-support/go-repo.ts::=BASE
  - harness/src/test-support/docs-repo.ts::=verifyDefaults
  - go:backend/internal/devload::TestALoadNeedsAKGItCanLeaseAndATruthItCanReadAndALoadAgainChangesNothing
  - go:backend/internal/devload::TestAReplayGivesTheSameHashesAndVersion
  - go:backend/internal/devload::TestAStoreThatRefusesStopsTheLoadWithTheBatchItReached
  - go:backend/internal/devload::TestATruthThatBreaksTheModelIsRefusedWithAReason
  - go:backend/internal/devload::TestAnObjectThatCannotFormItsKeyIsCountedWithTheLinksThatLoseAnEnd
  - go:backend/internal/devload::TestCountsMatchTheManifest
  - go:backend/internal/devload::TestEveryTruthObjectAndLinkIsReadableThroughTheQueries
  - go:backend/internal/devload::TestNeighborsAgreeWithTheTruth
  - go:backend/internal/devload::TestWhatNoSourceCanDeliverIsCountedAndLeftOut
  - go:backend/internal/graph::TestACardinalityBelongsToABindingNotToALinkType
  - go:backend/internal/graph::TestApplierConstructionAndRefusals
  - go:backend/internal/graph::TestBatchShapeRefusals
  - go:backend/internal/graph::TestCanonicalValuesIgnoreListOrderAndKeepConfidentialValuesOut
  - go:backend/internal/graph::TestChunkingCutsTheSameWorkAtAnyLimit
  - go:backend/internal/graph::TestChunksAreCutBySortedIDsAndDoNotDependOnInputOrder
  - go:backend/internal/graph::TestClosedListsOfRolesAndKinds
  - go:backend/internal/graph::TestCodesAreListedAndExplained
  - go:backend/internal/graph::TestContentHashSeesWhatMattersOnly
  - go:backend/internal/graph::TestEveryStoreFailureComesBackAsAnError
  - go:backend/internal/graph::TestHashStateKeepsPartsAndCleansUp
  - go:backend/internal/graph::TestIDsAreDerivedFromTheirParts
  - go:backend/internal/graph::TestKeyShapes
  - go:backend/internal/graph::TestLinkAndVersionIDs
  - go:backend/internal/graph::TestLinkAndVolatileValidation
  - go:backend/internal/graph::TestObjectValidationAgainstTheRegistry
  - go:backend/internal/graph::TestOrderEventsAndTheInputChain
  - go:backend/internal/graph::TestReleaseAndRestoreRefusals
  - go:backend/internal/graph::TestRenamingAndRenumberingTypesLeavesHashesAsTheyWere
  - go:backend/internal/graph::TestRetirementsChangesAndIdentifiersOnTheLiveStore
  - go:backend/internal/graph::TestRowValidityAndCloning
  - go:backend/internal/graph::TestSameBatchesGiveTheSameIdsNumbersHashesAndVersions
  - go:backend/internal/graph::TestScratchStoreSealsAndRefusesWhatItCannotDo
  - go:backend/internal/graph::TestScratchStoreServesWhatTheUnitsAboveItNeed
  - go:backend/internal/graph::TestSealRefusesWhileAnApplyIsOpen
  - go:backend/internal/graph::TestSealRulesOnTheLiveStore
  - go:backend/internal/graph::TestSumArithmeticIsExactAndOrderFree
  - go:backend/internal/graph::TestVerifyFindsAHashThatDoesNotMatchTheRows
  - go:backend/internal/graph::TestVolatileChangesLeaveTheHashAloneAndMakeLightLines
  - go:backend/internal/httpapi::TestAStoreThatFailsIsAServerProblem
  - go:backend/internal/httpapi::TestEveryAnswerFitsTheContract
  - go:backend/internal/httpapi::TestEveryOperationAsksForTheRoleItDeclares
  - go:backend/internal/httpapi::TestFindFiltersAndPages
  - go:backend/internal/httpapi::TestGetObjectAndItsLinks
  - go:backend/internal/httpapi::TestKGAnswersFitTheContract
  - go:backend/internal/httpapi::TestKGListAndOne
  - go:backend/internal/httpapi::TestKGReadsKeepTimeAndConditions
  - go:backend/internal/httpapi::TestKGRoutesWantReadAndAStore
  - go:backend/internal/httpapi::TestProblemsKeepTheirShapeAndCatalogue
  - go:backend/internal/httpapi::TestQuestionBodiesAreRefusedWithAProblem
  - go:backend/internal/httpapi::TestQuestionsAnswerLikeTheirShortForm
  - go:backend/internal/httpapi::TestRefusalsOfTheQueriesKeepTheirCatalogueCodes
  - go:backend/internal/httpapi::TestWithoutAnAuthenticatorOnlyTheOpenRoutesAnswer
  - go:backend/internal/memstore::TestAChangeOfEveryKindCanBeReadBackAtItsOwnNumber
  - go:backend/internal/memstore::TestARestoreRaisesTheGenerationAndCutsLaterRows
  - go:backend/internal/memstore::TestAnAbandonedApplyGoesAndItsNumbersComeBack
  - go:backend/internal/memstore::TestAnUnknownKGIsRefusedByEveryOperation
  - go:backend/internal/memstore::TestIdentifiersOfListsTheLiveIdentifiersOfOneObject
  - go:backend/internal/memstore::TestTheCatalogAndLeaseRefuseBadInput
  - go:backend/internal/memstore::TestWritesAndOldReadersAreHeldToTheirGenerationLeaseAndHead
  - go:backend/internal/query::TestFindExpandsSubtypesAndCountsByType
  - go:backend/internal/query::TestFindFiltersObjectsAndNeverMatchesOnAValueAboveTheCap
  - go:backend/internal/query::TestFindFiltersTableDriven
  - go:backend/internal/query::TestFindIsDeterministic
  - go:backend/internal/query::TestFindRefusesWhatIsNotServedYet
  - go:backend/internal/query::TestFindSummaryHasCountsOnly
  - go:backend/internal/query::TestGetEnvelopeGolden
  - go:backend/internal/query::TestGetHonoursViewAndMaxStaleness
  - go:backend/internal/query::TestGetMasksSensitiveAttributesAndHidesObjects
  - go:backend/internal/query::TestGetRefusesBadInputs
  - go:backend/internal/query::TestGetReturnsWhatTheIncludeListAsks
  - go:backend/internal/query::TestGrantsJudgeByLayerNamespaceAndScopeKeyConditions
  - go:backend/internal/query::TestNeighborsGroupsAndStepsAreReported
  - go:backend/internal/query::TestNeighborsLeaveHiddenObjectsOutAndCountThem
  - go:backend/internal/query::TestNeighborsPagesOfAHubRoundTrip
  - go:backend/internal/query::TestNeighborsRefusesBadInputs
  - go:backend/internal/query::TestNeighborsTableDriven
  - go:backend/internal/readcheck::TestAWorldLoadsToTheSameHashInBothStores
  - go:backend/internal/readcheck::TestFindPagesAtASealedVersionKeepTheVersion
  - go:backend/internal/readcheck::TestFindPagesRoundTripAndStayWhereTheyWereWhileTheKGMoves
  - go:backend/internal/readcheck::TestGetReadsThePastWithoutVolatileValues
  - go:backend/internal/readcheck::TestNeighborsReadThePastAndTheView
  - go:backend/internal/readcheck::TestReadsWaitForMinSeqAndNoticeANewGeneration
test_change_reason: 적합성 테스트의 사례 목록을 테스트 잠금의 지문에 포함해 사례를 삭제하거나 약화하면 잠금이 깨지게 했고(단언은 같고 사례별 요구사항 태그가 늘었다), Go의 상수 판정 규칙이 생겨 Go 기반 자료(fixture)가 상수 비교 대신 코드 호출 결과를 비교하게 했으며, 같은 메모의 재확인을 막아 문서 기반 자료가 이미 확인된 문서를 다시 스탬프하지 않게 했다. 세 곳 모두 테스트가 확인하는 것은 같거나 더 많다. 또 지원 코드(`graph/fixture.go`, `graph/scratch.go`)를 잠금 범위에 포함해 지문이 넓어졌다. 그 테스트들의 본문과 단언은 그대로다.
amends:
  - adr: "0019"
    what: 구조 계약의 `go:` 절에 두는 칸과 import 규칙
  - adr: "0023"
    what: Go 테스트 규칙의 이식과 잠금의 지문 범위
  - adr: "0024"
    what: "`core.kernel`이 `os`를 import하는 범위"
  - adr: "0025"
    what: 적합성 테스트 단위가 기준 구현을 import하는 범위
decision_hash: 6ab9c37a496b85ff
---
# ADR-0028 감사에서 나온 검사의 허점을 보완한다

## 맥락

PR #6부터 #13까지를 규칙 완화 관점에서 감사했다. 하한을 내리거나 허용 목록을 늘린 변경은 없었지만, 새 단위를 등록하면서 규칙에 허점을 함께 만든 곳, 새 검사에 허점이 있는 곳, 새 하한을 최솟값에 둔 곳, 요구사항 문구와 테스트가 어긋난 곳이 나왔다. 그 사이 PR #15가 `core.query`의 `adapter.memstore` import를 이미 제거했고 몇몇 하한을 올렸다. 이 결정은 남은 허점을 보완한다.

원칙은 하나다: **규칙, 한도, 허용 목록, 검사는 검사를 통과시키려고 느슨하게 하지 않는다.** 이 변경의 모든 항목은 규칙을 강화하거나 결함을 수정한다. 검사와 이미 확정된 결정이 어긋나면 결정이 규칙이고 검사를 그에 맞게 좁게 수정한다.

## 결정

1. **main의 CI가 푸시마다 실패하던 것을 수정한다(`ci.yml`).** `changes` 작업의 마지막 행 `[ -n "${others:-}" ] && ...`는 `others`가 비어 있으면 종료 코드 1이어서 작업이 실패했다. 푸시와 문서만 바뀐 풀 리퀘스트가 그렇다. `if ...; then ...; fi`로 바꿨다. 푸시와 풀 리퀘스트의 입력으로 스크립트를 그대로 추출해 실행해 확인했다. `test` 작업은 측정한 커버리지를 `quality.ts record`로 기록해 품질 등급표의 커버리지 열이 "알 수 없음"으로 남지 않게 한다.
2. **import 경로를 표기된 그대로 해석한다(`GOARCH054`).** `"o\x73"`는 `os`인데 하네스가 해석하지 못해 `GOARCH021`, `GOARCH050`, `GOARCH052`를 우회했다. 경로에 이스케이프나 패키지 경로에 쓰지 않는 문자가 있으면 위반이다(영문자, 숫자, `. _ ~ + - /`만 허용한다).
3. **테스트 파일에는 빌드 제약을 달지 못한다(`GOARCH055`).** `//go:build`, `// +build`, `_linux_test.go` 같은 OS·아키텍처 접미사가 달린 테스트는 잠금과 추적에는 잡히고 일부 환경에서는 실행되지 않는다. `//go:build ignore`도 테스트 파일에서는 같다.
4. **적합성 테스트의 사례 목록이 잠금에 포함된다.** 구조 계약의 `go.locked_support`(패키지 폴더 또는 파일)에 적은 비테스트 코드는, 그것을 `package.Name`으로 호출하는 테스트(그 폴더의 테스트는 이름만으로도)의 지문에 포함된다. 지금은 `backend/internal/graphcheck`와 테스트가 쓰는 `backend/internal/graph/fixture.go`(`Fixture`), `backend/internal/graph/scratch.go`(`ScratchStore`)다. 파일 둘을 넣어 지문이 넓어진 테스트 83개는 지원 코드를 잠금 범위에 포함해 지문이 넓어졌을 뿐 본문과 단언이 그대로이고 모두 `amends_tests`에 있다. 사례를 삭제하거나 단언을 약화하거나 추가하면 `TestReferenceStorePassesTheSuite`의 `TLOCK001`이 실패한다. 사례 31개의 이름과 순서를 `TestSuiteCaseNamesAreFixed`가 별도로 고정하고, 사례마다 `[REQ-NNN]`을 단 하위 테스트로 실행해 REQ-249의 사례(분할 적용, 이어서 적용, `building`, 대기)가 자기 이름으로 추적된다.
5. **테스트에서만 허용되는 import를 별도로 적는다(`test_may_import`).** 제품 코드는 `may_import`만, `_test.go`는 `may_import`와 `test_may_import`를 쓴다. 제품 코드가 테스트 전용 단위를 import하면 `GOARCH010`(메시지가 "테스트 파일에서만"이라고 밝힌다), 두 목록에 같은 이름이 있으면 `GOARCH005`, 존재하지 않는 단위면 `GOARCH004`다. 이동한 허용: `adapter.graphcheck`의 `adapter.memstore`, `adapter.devload`의 `core.query`, `adapter.readcheck`의 모든 허용(그 단위는 테스트 파일만 있어 제품 코드의 `may_import`가 빈 목록이다). 제품 코드의 import는 이 변경 전에 이미 이 형태였고 `GOARCH010`이 그것을 확인한다.
6. **`os`는 `entropy.go` 한 파일만 import한다.** ADR-0024가 환경 변수를 읽는 유일한 진입점을 `entropy.go`의 `LookupEnv`로 정했고(ADR-0019의 지정 파일 규칙과 같은 위치다), 그 결정 때문에 `core.kernel` 단위 전체에 `os` import가 허용되었다. 확정된 결정은 진입점이 `entropy.go`라는 것이므로 환경 변수 읽기를 조립부로 옮기지 않고 **제한을 파일 단위로 좁힌다**: `restricted_stdlib`의 항목이 `.go`로 끝나면 그 파일 하나다. `os: [cmd.praxium, backend/internal/kernel/entropy.go]`. 존재하지 않는 단위나 파일은 `GOARCH006`이다.
7. **커버리지 하한이 측정값을 따라가게 한다(`COV006`, `GOCOV003`).** 하한이 `pnpm ratchet`이 정할 값(측정값을 0.5 단위로 내림하고 98을 상한으로 한 값)보다 1점(`MAX_FLOOR_LAG`) 넘게 낮으면 실패한다. 래칫이 하한을 올리지 않던 규모가 작은 단위의 기준도 강화했다: 측정 대상 문장 수가 50 미만이면 최솟값에 두던 것을 15 미만으로 낮췄다(`MIN_MEASURED_FOR_RATCHET`). 그래서 문장 18개의 `adapter.devauth`(하한 90, 측정값 100)와 26개의 `core.access`(90, 96.2)도 측정값을 따른다. 이 변경에서 `pnpm ratchet`을 실행해 모든 하한을 그 값에 일치시켰다(`go:adapter.devauth` 90 -> 98, `go:core.access` 90 -> 96, 나머지는 이미 같았다). 하한은 내려가지 않는다(`COV003`).
8. **Go에도 TEST003~TEST005에 대응하는 규칙을 둔다.** `GOTEST004`는 상수로 정해지는 조건(`if 1+1 != 2`, `if x != x`), `GOTEST005`는 에러를 검사하고 실패 없이 반환하는 `if err != nil { return }`와 실패 경로가 없는 `recover()`, `GOTEST006`은 `m.Run`을 호출하지 않는 `TestMain`과 `test.run` 같은 플래그를 설정하는 코드다. 대응하는 규칙이 없는 것은 없다. 이름으로 참조하는 상수와 `_ =`로 버린 에러는 아직 분석하지 않는다(TD-0015). ADR-0023 4항이 `GOTEST003`을 통과시키려고 바꿨던 바탕 자료(`go-repo.ts`의 `BASE`)의 `if 1+1 != 2`를 코드 호출 결과 비교(`len("ab") != 2`)로 되돌린다.
9. **요구사항 문구.** REQ-241에서 테스트가 측정하지 못하는 기간 조항(한 메이저 버전 동안)을 분리해 **예정**인 REQ-262로 만들었다. REQ-229의 "권한에 맞게 거른 목록"은 테스트가 확인하는 것(필터링한 목록)으로 되돌렸다. REQ-249의 사례는 하위 테스트 이름에 태그를 달았다.
10. **문서 재확인 스탬프를 엄격하게 한다.** `reverify`는 같은 대상의 이전 기록과 같은 메모를 받지 않고, 한 번에 10개를 넘는 대상은 `--target`으로 나눠 각각 자기 메모를 쓰게 한다. 생성물의 머리글 "5시간마다"는 정리 작업의 계획(하루에 한 번)에 맞게 수정했다.
11. **컴파일된 실행 파일을 커밋하지 못한다(`GOARCH056`).** 커밋할 수 있는 위치(추적 중이거나 `.gitignore`가 제외하지 않는 새 파일)에 실행 파일의 헤더 시그니처가 있거나 `*.test`면 위반이다. `.gitignore`에 `backend/cmd/praxium/praxium`, `backend/**/*.exe`, `*.test`를 더했다.
12. **남기는 항목은 주제별 부채로 등록했다.** TD-0012(넓은 테스트 태그), TD-0015(Go 테스트 규칙의 남은 구멍), TD-0016(다시 확인의 증거), TD-0017(품질 등급표의 운영), TD-0018(스스로 선언하는 면제). REQ-262(기한을 계산하는 일)는 예정 요구사항 자체가 추적하므로 부채로 따로 두지 않는다. 작업 중 커밋에서 테스트를 먼저 잠그는 번거로움은 [ADR 작성 절차](../adr-process.md)에 절차로 적었다.

### 변경된 잠긴 테스트

| 키 | 이전 | 새 | 같거나 더 엄한 이유 |
|---|---|---|---|
| `go:backend/internal/graphcheck::TestReferenceStorePassesTheSuite` | 31개 사례를 `t.Run(c.Name)`으로 실행한다 | 사례의 수와 이름과 순서를 목록과 대조하고, 하위 테스트 이름에 사례마다 `[REQ-NNN]`을 단다. 지문에 `graphcheck`의 사례 목록이 포함된다 | 단언은 그대로이고 확인이 늘었다 |
| `harness/src/test-support/go-repo.ts::=BASE` | `cmd/app`의 테스트가 `if 1+1 != 2`로 실패를 발생시킨다 | `if len("ab") != 2`(호출 결과 비교) | 새 `GOTEST004`가 이전 형태를 잡는다. 기반 자료가 그 규칙을 지킨다 |
| `harness/src/test-support/docs-repo.ts::=verifyDefaults` | 호출될 때마다 모든 기본 문서를 같은 메모로 다시 확인한다 | 이미 확인된 문서는 다시 스탬프하지 않고, 낡은 것만 대상의 해시를 담은 새 메모로 다시 확인한다 | 새 규칙이 같은 메모를 막는다. 확인되는 문서의 집합은 같다 |

## 이유

- 감사가 재현한 허점(이스케이프, 빌드 제약, 사례 목록)은 규칙을 한 줄로 우회할 수 있었다. 우회 경로를 막는 것은 규칙을 강화하는 일이다.
- 테스트 전용 허용을 단위 전체의 `may_import`에 두면 제품 코드가 그 단위를 import해도 막히지 않는다. 목록을 나누면 같은 사실(테스트에서만 쓴다)을 문장이 아니라 도구가 지킨다.
- `os` 제한을 파일로 좁히는 쪽이 확정된 결정(진입점은 `entropy.go`)을 따르고, 조립부로 옮기는 것보다 결정을 뒤집지 않는다. 단위 전체에서 파일 하나로 좁아졌으니 규칙은 이전보다 엄격하다.
- 하한이 측정값을 따라가지 못하면 커버리지가 올랐다가 다시 떨어져도 통과한다.

## 대안과 버린 이유

- 환경 변수 읽기를 `cmd/praxium`으로 옮기고 `os` import를 `cmd.praxium`만으로 제한하기: ADR-0024가 `entropy.go`를 진입점으로 정한 결정과 어긋나서 버렸다. 파일 제한이 같은 보호를 제공한다.
- 지원 코드를 선언 없이 모든 비테스트 코드까지 넓히기: 같은 이름만 맞아도 무관한 테스트가 잠금에 묶여 테스트 수십 개의 잠금을 다시 갱신해야 한다. 선언한 폴더만 `package.Name`으로 호출될 때 지문에 포함한다.
- 빌드 제약이 있는 테스트 파일을 `GOIGN`의 이유 규칙으로 허용하기: 허용하는 경로가 열려 있는 한 같은 허점이 남는다. 쓰는 곳이 0이라 금지로 정했다.
- 하한 지연을 알림으로만 두기: 알림은 아무도 수정하지 않는다. 실패로 처리했다.

## 결과

- 새 코드: `GOARCH005`, `GOARCH006`, `GOARCH054`, `GOARCH055`, `GOARCH056`, `GOTEST004`~`GOTEST006`, `COV006`, `GOCOV003`. 새 설정 필드: `test_may_import`, `locked_support`, `restricted_stdlib`의 파일 항목.
- `harness/config/`와 `.github/`의 모든 변경은 규칙을 강화하는 쪽이다: 새 필드와 항목, 좁아진 `os`, 올라간 하한, 늘어난 단계. 완화된 것은 없다.
- 적합성 테스트의 사례를 추가하거나 수정하려면 `TestReferenceStorePassesTheSuite`와 `TestSuiteCaseNamesAreFixed`의 키를 `amends_tests`에 적는다([ADR 작성 절차](../adr-process.md)).
