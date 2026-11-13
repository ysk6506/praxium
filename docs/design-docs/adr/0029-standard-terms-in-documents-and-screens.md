---
title: ADR-0029 문서와 화면의 문구를 표준 용어로 통일하고, 옛 표기가 다시 쓰이지 않게 검사한다
kind: decision
status: active
enforced_by:
  - WORD001
  - WORD003
  - WORD004
  - TLOCK001
  - DOC096
  - harness/src/checks/doc-terms.test.ts
  - harness/src/checks/ui-words.test.ts
updates_docs:
  - AGENTS.md
  - ARCHITECTURE.md
  - README.md
  - docs/design-docs/access-control.md
  - docs/design-docs/adr-process.md
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
  - docs/exec-plans/tech-debt.yaml
  - docs/index.md
  - docs/product-specs/requirements.md
  - docs/references/design-research.md
  - docs/references/from-terrarium.md
  - docs/references/owner-requirements-from-terrarium.md
amends:
  - adr: "0002"
    what: 화면 문구에 대한 작업 규칙을 용어집의 표준 용어로 바꾸고 문서 본문에도 적용한다
  - adr: "0004"
    what: 목표 문장의 표기를 표준 용어로 고치고 사내를 뺀다. 결정은 그대로다
touches_protected:
  - harness/**
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
amends_tests:
  - harness/src/checks/doc-amends.test.ts::link-amends copies the phrase to the amended ADR [REQ-020] > the generated list shows the phrase beside the amending ADR
  - harness/src/checks/doc-audit.test.ts::test rows [REQ-020] > =table
  - harness/src/checks/doc-audit.test.ts::test rows [REQ-020] > a table with a 시험 파일 column is judged by that column, with brace paths expanded
  - harness/src/checks/doc-decisions.test.ts::the decisions table is made from ADR front matter [REQ-020] > one line per ADR in number order with status and who changed it
  - harness/src/checks/go-deps.test.ts::=table
  - "harness/src/checks/go-deps.test.ts::Go modules: go.mod is read [REQ-239] > table rows are the ones under the module, licence, reason header"
  - harness/src/checks/quality-go.test.ts::Go units in the quality table [REQ-024] > a Go unit with no recorded coverage is unmeasured, not zero
  - harness/src/checks/quality.test.ts::quality grades follow from measured values [REQ-024] > the reasons name what to fix, worst class first; an A has none
  - harness/src/checks/quality.test.ts::quality grades follow from measured values [REQ-024] > the table prints the rule above the rows, and `모름` for a unit without a record
  - harness/src/checks/quality.test.ts::recorded coverage feeds the table, nothing is invented [REQ-024] > the CLI writes, records and checks
  - harness/src/checks/quality.test.ts::recorded coverage feeds the table, nothing is invented [REQ-024] > without a record every measured unit is unknown, units without a floor show a dash
  - harness/src/checks/quality.test.ts::the quality table is generated and kept fresh [REQ-024] > =STALE
  - "harness/src/checks/ui-words.test.ts::the word list: old words on screen fail with the word to use [REQ-003] > %s"
  - "harness/src/checks/ui-words.test.ts::the word list: old words on screen fail with the word to use [REQ-003] > %s#2"
  - "harness/src/checks/ui-words.test.ts::the word list: old words on screen fail with the word to use [REQ-003] > it names the line, the found word and the word to use"
  - harness/src/test-support/go-repo.ts::=GO_ARCHITECTURE_DOC
test_change_reason: 표준 용어를 금지하던 규칙(회, 횟수, 시드, 에스컬레이션)을 없애거나 뒤집고, 하네스가 읽는 표 머리글(`시험 파일`을 `테스트 파일`로, `까닭`을 `이유`로)과 생성 문서의 말을 표준 용어로 바꿨다. 단언의 힘은 그대로이고 기대하는 문자열만 바뀌었다.
decision_hash: 766ca73ec8ee0fa6
---
# ADR-0029 문서와 화면의 문구를 표준 용어로 통일하고, 옛 표기가 다시 쓰이지 않게 검사한다

## 맥락
문서 곳곳에 우리가 만들어 낸 용어가 쓰였다. 예를 들면 `봉하다`, `열쇠`, `후보 실행`, `투영`, `건강`, `운영 세계` 같은 말이다. 한국의 개발자와 인프라 엔지니어가 쓰는 용어(확정, 키, 드라이 런, 프로젝션, 상태, KG)가 따로 있는데도 그렇다. 처음 보는 사람은 이 문서를 읽기 전에 사전부터 익혀야 한다. 쉬운 말을 쓰라는 규칙(ADR-0002)이 새로 만든 용어를 부추긴 면도 있다. 또 화면 문구를 검사하는 `ui-words`는 표준 용어인 "회", "횟수", "시드", "에스컬레이션"을 오히려 금지하고 있었다.

## 결정
1. **원칙.** 문서와 화면에는 한국의 IT 실무자가 실제로 쓰는 표준 용어를 쓴다. 그중 쉽고 널리 쓰이는 것을 고른다. 영어로 쓰는 것이 보통인 말(API, ETag, KG, SSE, OIDC, CI, upsert)은 영어로 둔다. 원래 자연스러운 우리말(객체, 링크, 속성, 요구사항, 결정 기록)은 그대로 둔다. 새 말을 지어내지 않는다.
2. **용어집이 하나뿐인 사전이다.** [용어집](../glossary.md)과 [일반 용어](../glossary-general.md)의 표(열: 표준 용어 / 옛 표기(쓰지 않음) / 뜻 / 영어 대응)가 사전의 단일 출처다. 용어를 추가하거나 수정하려면 그 표를 수정한다. 검사가 그 표를 직접 읽으므로 사람이 읽는 표와 검사하는 목록이 어긋날 수 없다.
3. **ADR 본문도 표기만 고친다.** 결정의 내용, 숫자, 식별자, 코드, 경로, 요구사항 번호, 절 번호와 순서는 바꾸지 않는다. 수정한 ADR은 이 ADR이 다시 확정(seal)한다. 이 ADR의 `amends`는 앞선 결정의 *표기 규칙*을 바꾸는 두 곳(ADR-0002, ADR-0004)만 적는다. 표기만 수정한 나머지 ADR은 `touches_protected`에 적는다.
4. **참조 문서는 고치지 않는다.** `docs/references/`의 출발 자료(`from-terrarium.md`, `owner-requirements-from-terrarium.md`)는 원문을 옮긴 것이라 표기를 수정하지 않고, 머리말에 "이 문서의 표기는 당시의 것이고 현재 표준은 용어집"이라고 적는다. 우리가 쓴 `design-research.md`는 수정한다.
5. **문서 용어 검사(`WORD003`, `WORD004`).** 기존 `ui-words` 검사 안에서 돈다(검사 이름을 더하지 않는다). 설정은 `harness/config/ui-words.yaml`의 `documents:` 절이다.
   - 사전은 용어집 표를 읽어 만든다. 옛 표기 열은 쉼표로 구분하고, 문자열 그대로(부분 문자열) 찾는다.
   - 건너뛰는 곳: 코드 블록, 인라인 코드, 링크 대상과 URL, HTML 주석, `>` 인용 줄, 앞부분(front matter)의 `title`·`what`·`*_reason`이 아닌 키, 참조 문서 둘, 용어집 표의 옛 표기 열.
   - 의미가 여러 가지인 쉬운 말(길, 일, 자리, 기록 같은 것)은 `ambiguous_words`에 적고 사전의 항목으로 받지 않는다. 그런 말은 의미가 분명한 합성어(예: "변경 경로")로만 사전에 올린다. 문맥별 표준 용어는 용어집 끝의 "뜻이 갈리는 말" 표에 있다.
   - 표준 용어가 옛 표기를 포함하는 경우(단일 진실 공급원, 공급망, 호환성을 깨는 변경)는 `keep`에 적어 통과시킨다.
   - `WORD003`은 문서에 옛 표기가 있을 때, `WORD004`는 사전을 못 읽거나 항목이 규칙을 어길 때(표 형식 오류, 중복, 옛 표기와 표준 용어가 같음, `ambiguous_words`에 든 말) 난다.
   - **모드.** `documents.mode`가 `report`이면 문서별 개수를 알림(NOTE)으로만 표시하고 실패하지 않는다(`pnpm tsx harness/src/checks/ui-words.ts --documents`로 문서별 목록, `--detail`로 줄 번호까지). 전환이 끝나는 마지막 단계에서 같은 키를 `fail`로 바꾸면 `WORD003`이 실패로 바뀐다. 이것은 검사가 느슨해지는 것이 아니라 재발을 막는 강화다.
6. **옛 규칙을 없애거나 뒤집는다.** 표준 용어를 금지하던 `회`, `횟수` 규칙은 삭제하고, `seed`(시드)는 옛 표기 `씨앗`을 막는 쪽으로, `escalation`(에스컬레이션)은 옛 표기 `알림 넘기기` 계열을 막는 쪽으로 뒤집는다. 금지 대상이 표준 용어에서 옛 표기로 바뀌었을 뿐 검사의 강도는 줄지 않았다.
7. **하네스가 읽는 표지.** 테스트 표의 열 이름 `시험 파일`을 `테스트 파일`로, 표 제목에 들어가는 말 `시험`을 `테스트`로(`doc-tests.ts`, `DOC096`), Go 의존 표 머리글 `까닭`을 `이유`로(`go-deps.ts`) 바꾼다. 표지를 쓰는 문서는 같은 변경에서 수정한다. 생성 문서의 한국어 문구(결정 목록, 품질 표, 제3자 고지, 색인의 그룹 제목)도 표준 용어로 바꾸고, 생성 문서는 명령으로 다시 만든다.
8. **요구사항.** REQ-002는 "현장 표준 용어와 쉬운 문장", REQ-003은 "옛 표기가 다시 쓰이면 검사가 알린다"로 수정한다. 요구사항의 의미는 그대로다.
9. **쓰기 규칙 문서.** AGENTS.md, ADR 작성 절차, 하네스 안내, 핵심 신념에서 "쉬운 말" 규칙을 "용어집의 표준 용어" 규칙으로 바꾼다.
10. **제품 목표 문장.** 요구사항, README, AGENTS.md에 같은 한 줄이 있고 이번에 표기를 수정한다. 인용한 ADR-0001, ADR-0004도 같은 문장으로 바꾼다(결정은 그대로).

    > 옛: 현실의 IT 인프라를 데이터로 받아 운영 세계를 짓고, 그 위에서 에이전트가 맡는 E2E 프로세스를 정하고 맡기고 재는 사내 운영 관리 플랫폼.
    >
    > 새: 실제 IT 인프라를 믿을 수 있는 지식 그래프(KG)로 만들고, 그 위에서 AI 에이전트가 수행하는 엔드투엔드(E2E) 운영 업무를 정의·위임·측정하는 오픈소스 플랫폼.

    바뀐 것 4가지:
    - 표기를 표준 용어로 수정했다(`운영 세계`는 KG, `맡김`은 위임, `재다`는 측정).
    - "사내"를 뺐다. 제품 책임자가 범용 제품으로 정하고 오픈소스로 공개하기로 했다. 범위에 대한 결정은 뒤따르는 ADR이 다룬다.
    - "믿을 수 있는"을 추가했다. 이미 있는 근거 등급과 완전성 선언의 설계를 가리키는 말이고 새 요구가 아니다.
    - "엔드투엔드(E2E) 프로세스"를 "엔드투엔드(E2E) 운영 업무"로 풀었다. 용어집의 표준 용어는 "E2E 프로세스"로 둔다.

    README는 목표 문장 바로 아래에 "인프라는 지식 그래프로, 운영은 에이전트에게."라는 한 줄을 둔다.
11. **문서 언어.** 문서는 한국어로 쓴다. 영어 문서는 오픈 베타 이후 제품이 안정되면 제공한다. 그때까지 영어 문서를 만들지 않는다. 용어집의 영어 대응 열은 그때의 번역 사전으로 쓴다. 영어 제공의 방법은 그 시점에 정한다.
12. **이번 변경에서 고치지 않는 것.** 남은 "사내"와 한 조직만 가정한 문장은 이 변경에서 고치지 않고, "사내"를 새로 쓰지도 않는다. 뒤따르는 방향 ADR이 다룬다.

## 이유
- 표준 용어를 쓰면 처음 읽는 사람이 사전 없이 읽을 수 있고, 검색 결과나 외부 문서와도 용어가 일치한다.
- 사전의 출처를 사람이 읽는 표 하나로 두면 두 목록이 어긋나지 않는다. 표가 깨지면 `WORD004`가 바로 실패한다.
- 처음에는 보고 모드로 개수만 표시하므로, 문서를 수정하는 동안 변경이 차단되지 않고 끝나면 실패로 전환한다.
- ADR 본문을 수정하지 않으면 옛 표기가 문서의 가장 큰 부분에 남아 사전과 검사가 서로 어긋난다. 표기만 수정하므로 결정은 그대로다.

## 버린 대안
- **금지어 목록을 설정 파일에 따로 두기**: 용어집과 어긋난다.
- **처음부터 실패로 전환하기**: 문서 70개가 한 변경에 걸려 수정과 검사 구현을 나눌 수 없다.
- **ADR 본문은 그대로 두기**: 옛 표기가 계속 읽히고, 검사에 ADR만 예외를 두면 건너뛰는 곳이 늘어난다.
- **참조 문서도 수정하기**: 원문을 옮긴 문서라 당시의 표기가 근거 자료다.

## 결과
- 새 파일: `harness/src/checks/doc-terms.ts`와 테스트, 이 ADR, 일반 용어집.
- 잠긴 테스트는 `amends_tests`의 키만 바뀌었다. 바뀐 점은 기대하는 문자열(`시험 파일`을 `테스트 파일`로, `까닭`을 `이유`로, 생성 문서의 문구)와, `ui-words` 규칙 목록에서 회, 횟수, 시드, 에스컬레이션 항목을 삭제하거나 뒤집은 것이다. 삭제한 항목은 표준 용어를 금지하던 것이라 이 ADR이 근거다.
- `harness/config/ui-words.yaml`: `documents:` 절 신설(등록, 강화), `회`와 `횟수` 금지 삭제(잘못된 금지의 철회), `seed`와 `escalation` 뒤집기. 기존 금지를 풀어 주는 변경은 없다.
- 남은 옛 표기는 보고 모드의 개수로 확인하고, 문서 그룹별로 나누어 수정한 뒤 `documents.mode`를 `fail`로 바꾼다.
