---
title: ADR-0030 문서 용어 검사를 실패 모드로 전환한다 — 정비를 마쳤으므로 옛 표기가 다시 쓰이면 병합할 수 없다
kind: decision
status: active
enforced_by:
  - WORD003
  - WORD004
  - harness/src/checks/doc-terms.test.ts
  - harness/src/checks/ui-words.test.ts
updates_docs:
  - ARCHITECTURE.md
  - README.md
  - docs/design-docs/access-control.md
  - docs/design-docs/adr-process.md
  - docs/design-docs/competency-examples.md
  - docs/design-docs/competency-questions.md
  - docs/design-docs/core-beliefs.md
  - docs/design-docs/data-observability.md
  - docs/design-docs/defaults.md
  - docs/design-docs/definitions-at-agent-scale.md
  - docs/design-docs/definitions-lineage.md
  - docs/design-docs/definitions.md
  - docs/design-docs/glossary-general.md
  - docs/design-docs/glossary.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/ingestion-extensions.md
  - docs/design-docs/ingestion.md
  - docs/design-docs/kg-model.md
  - docs/design-docs/kg-operations.md
  - docs/design-docs/kg-queries-examples.md
  - docs/design-docs/kg-queries.md
  - docs/design-docs/kg-read-measurements.md
  - docs/design-docs/kg-storage-apply.md
  - docs/design-docs/kg-storage.md
  - docs/design-docs/pipeline.md
  - docs/design-docs/postgres-operations.md
  - docs/design-docs/process-direction.md
  - docs/design-docs/product-design.md
  - docs/design-docs/query-engine-shape.md
  - docs/design-docs/rest-api-feature-routes.md
  - docs/design-docs/rest-api-resources-additions.md
  - docs/design-docs/rest-api-resources-data.md
  - docs/design-docs/rest-api-resources-graph.md
  - docs/design-docs/rest-api-walkthroughs.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/design-docs/self-observability.md
  - docs/design-docs/standard-model.md
  - docs/design-docs/temporary-console.md
  - docs/design-docs/worldgen.md
  - docs/exec-plans/active/plan.md
  - docs/index.md
  - docs/references/design-research.md
amends:
  - adr: "0029"
    what: 문서 용어 검사의 모드를 보고에서 실패로 전환한다
touches_protected:
  - harness/config/ui-words.yaml
  - AGENTS.md
  - docs/product-specs/requirements.md
  - docs/design-docs/adr/0001-product-goal-draft-and-seams-with-terrarium.md
  - docs/design-docs/adr/0002-harness-and-working-rules-from-terrarium.md
  - docs/design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md
  - docs/design-docs/adr/0004-goal-confirmed-and-handover-from-terrarium.md
  - docs/design-docs/adr/0005-separate-product-own-model-verification-outside.md
  - docs/design-docs/adr/0006-go-backend-clean-architecture-backend-first.md
  - docs/design-docs/adr/0007-kg-model.md
  - docs/design-docs/adr/0008-ingestion.md
  - docs/design-docs/adr/0009-kg-storage-and-versions.md
  - docs/design-docs/adr/0010-kg-queries-and-impact.md
  - docs/design-docs/adr/0011-kg-operations.md
  - docs/design-docs/adr/0012-access-control.md
  - docs/design-docs/adr/0013-data-observability.md
  - docs/design-docs/adr/0014-definitions-branches-revert.md
  - docs/design-docs/adr/0015-competency-questions.md
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
  - docs/design-docs/adr/0017-self-observability-and-operability.md
  - docs/design-docs/adr/0018-secrets-and-encryption.md
  - docs/design-docs/adr/0019-harness-reads-go-backend-place-and-checks.md
  - docs/design-docs/adr/0020-type-registry-and-standard-model-data.md
  - docs/design-docs/adr/0021-synthetic-world-generator.md
  - docs/design-docs/adr/0022-standard-model-fitted-to-impact-scope.md
  - docs/design-docs/adr/0023-harness-locks-go-tests-and-links-them-to-requirements.md
  - docs/design-docs/adr/0024-rest-contract-becomes-a-document-the-machine-keeps.md
  - docs/design-docs/adr/0025-kg-stands-on-a-sequence-of-changes-storage-port-and-reference-implementation.md
  - docs/design-docs/adr/0026-closed-queries-first-three-stand-as-the-reference.md
  - docs/design-docs/adr/0027-reading-the-kg-through-the-api-and-the-command-line.md
  - docs/design-docs/adr/0028-close-the-gaps-the-audit-found-in-the-checks.md
  - docs/design-docs/adr/0029-standard-terms-in-documents-and-screens.md
amends_tests: []
decision_hash: 471007ca14918ba7
---
# ADR-0030 문서 용어 검사를 실패 모드로 전환한다 — 정비를 마쳤으므로 옛 표기가 다시 쓰이면 병합할 수 없다

## 맥락
ADR-0029는 문서와 화면에 표준 용어를 쓰기로 하고, 문서 용어 검사(`WORD003`, `WORD004`)를 보고 모드로 먼저 도입했다. 보고 모드는 문서별 옛 표기 개수만 알리고 실패하지 않는다. 문서 전체를 수정하는 동안 변경이 차단되지 않게 하려는 것이었고, 정비가 끝나면 같은 설정 키를 `fail`로 바꾸기로 했다.

정비는 두 단계로 진행했다. 1단계는 뜻이 분명한 용어를 기계적으로 치환했다. 2단계는 사람이 읽고 뜻이 갈리는 말과 부자연스러운 문장을 수정했다. 2단계에서 문서 사이의 용어를 통일하고, 치환이 남긴 손상(같은 말이 겹치거나 조사가 깨진 곳)과 몇 가지 표기 오류를 바로잡았다. 보고 모드로 센 옛 표기는 0건이 됐다.

## 결정
1. **`harness/config/ui-words.yaml`의 `documents.mode`를 `report`에서 `fail`로 바꾼다.** 이 한 줄이 이 결정이 바꾸는 설정의 전부다. 사전, `ambiguous_words`, `keep`, 건너뛰는 문서, 검사 코드와 테스트는 바꾸지 않는다. 이후 문서에 옛 표기가 다시 나오면 `WORD003`이 실패하고, 검사를 통과시키려고 사전이나 설정을 느슨하게 바꾸지 않는다. 새 용어가 필요하면 용어집의 표를 수정한다(ADR-0029의 2).
2. **2단계에서 수정한 ADR은 표기만 수정하고 이 ADR이 다시 확정한다.** 결정의 내용, 숫자, 식별자, 요구사항 번호, 절 번호와 순서는 바꾸지 않았다. 수정한 ADR은 `touches_protected`에 적는다. 앞선 결정을 바꾸는 곳은 ADR-0029의 모드 단계 하나뿐이어서 `amends`는 그것만 적는다.
3. **ADR의 본문에서 사실과 어긋난 표기 두 곳을 결정에 맞췄다.** ADR-0020은 고정 채널을 한 곳에서 4종, 다른 곳에서 6종이라고 적었다. 표준 모델의 데이터와 ADR-0007의 결정이 6종이므로 6종으로 통일했다. ADR-0026은 읽기 워터마크를 가리켜야 할 두 곳에서 소스 커서라고 적었다. 그 두 곳을 읽기 워터마크로 맞췄다.
4. **용어를 통일하고 용어집에 반영했다.** 건수 대조(옛 표기 `수 맞춤`), 정합성 대조(`정합 대조`), KG 질의(`KG에 묻기`), 리비전(항목의 `판`), 보충 문서(`덧붙임 문서`)를 용어집에 추가하거나 옛 표기 열에 올렸다. 헬스 상태는 6종이며 `affected_upstream`을 포함한다고 용어집에 적었다. 메시지 로그 스토리지, 롤백 액션, 리버트의 구분도 문서 전체에서 하나로 맞췄다. 롤백은 상태를 이전 시점으로 되감는 일과 되감을 수 있는 운영 액션에만 쓰고, 리버트는 새 변경으로 이전 변경을 취소하는 일에만 쓴다.
5. **문서의 사실 오류 몇 곳을 바로잡았다.** `worldgen.md`는 MLAG를 생성하지 않는다고 적으면서 `MlagDomain`을 생성한다고도 적었다. 코드(`backend/internal/worldgen`)가 `MlagDomain`을 생성하므로 생성하는 쪽으로 맞췄다. `requirements.md`의 번호 영역 설명은 218~240이라고 적었지만 표는 REQ-262까지다. 범위 표기만 실제에 맞췄다. REST API 예시 문서의 명령줄 도구 이름은 실제 실행 파일 이름 `praxium`으로 바꿨다. ADR 작성 절차의 테스트 표 열 이름은 하네스가 읽는 `테스트 파일`로 맞췄다.

## 이유
- 보고 모드로는 옛 표기가 다시 들어와도 변경이 병합된다. 전환이 끝났으므로 재발을 막는 것은 실패 모드뿐이다.
- 사전의 출처가 용어집의 표 하나이므로, 실패 모드에서도 용어를 추가하는 절차가 단순하다. 표에 올리면 바로 쓸 수 있다.
- 설정 한 줄 외에는 아무것도 바꾸지 않아서, 검사를 느슨하게 하지 않고 강화하는 변경이라는 것을 확인하기 쉽다.

## 버린 대안
- **보고 모드를 계속 유지하기**: 개수를 알리기만 해서 재발을 막지 못한다.
- **일부 문서만 실패로 전환하기**: 문서마다 모드를 두려면 설정과 검사 코드를 바꿔야 하고, 예외가 늘어난다. 전체가 0건이므로 필요하지 않다.
- **용어 정비와 모드 전환을 다른 변경으로 나누기**: 정비가 끝난 상태와 전환이 같은 변경에 있어야 `pnpm check`가 정비 결과를 바로 검증한다.

## 결과
- `harness/config/ui-words.yaml`: `documents.mode`가 `fail`이다. 다른 변경은 없다. 하네스의 소스와 테스트는 바뀌지 않았다.
- 문서에 옛 표기가 있으면 `WORD003`이 실패하고 쓸 용어를 알려 준다. 문서를 수정할 때는 용어집의 표를 먼저 본다.
- 이 변경은 문서와 ADR의 표기를 수정하므로 본문이 바뀐 ADR은 모두 다시 확정했고, 확인 기록(`docs/verified/`)과 생성 문서는 명령으로 다시 만들었다.
