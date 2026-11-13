---
title: ADR 작성 절차
kind: guide
status: active
covers:
  - harness/config/docs.yaml
  - harness/src/checks/doc-adr.ts
  - harness/src/checks/doc-follow.ts
  - harness/src/checks/doc-diff.ts
  - harness/src/checks/doc-decisions.ts
  - harness/src/checks/doc-amends.ts
  - harness/src/checks/doc-claims.ts
  - harness/src/checks/doc-facts.ts
  - harness/src/checks/doc-tests.ts
  - harness/src/checks/doc-audit.ts
  - harness/src/checks/doc-records.ts
  - harness/src/checks/doc-freshness.ts
  - harness/src/checks/doc-targets.ts
  - harness/src/checks/doc-legacy.ts
  - harness/src/checks/doc-index.ts
---
# ADR (Architecture Decision Record)

결정은 저장소에 남아야 한다. 채팅·머릿속의 결정은 다음 에이전트에게 존재하지 않는다.

## 언제 쓰나

- 새 레이어, 새 외부 라이브러리, 의존 규칙 변경
- 하네스(`harness/`), 설정 파일(`package.json`, `vitest.config.ts` 등), CI 설정(`.github/`), 결정 기록, 요구사항, AGENTS.md 변경 (보호 경로. 전체 목록은 `harness/config/protected-paths.txt`)
- 요구사항이 바뀌어 잠긴 테스트를 바꿔야 할 때
- 설계에서 여러 길 중 하나를 고를 때

## 만드는 법

1. `docs/design-docs/adr/NNNN-짧은-이름.md` 생성. 다음 번호를 쓴다.
2. 본문은 쉬운 말로 **맥락 → 결정 → 이유 → 대안과 버린 이유 → 결과**.
3. 앞부분(front matter)에 아래를 적는다.

```yaml
title: ...
kind: decision
status: active          # active | superseded
enforced_by: [ARCH010]  # 규칙 코드 또는 테스트 경로. 못 하면 [none] + no_enforcement_reason
touches_protected: []   # 이 ADR로 바꾸는 보호 경로 (harness/**, package.json ...)
amends_tests: []        # 요구사항 변경으로 바꾸는 잠긴 테스트 키 (Go는 go:<디렉터리>::<함수>)
test_change_reason: ""  # amends_tests를 쓸 때: 왜 요구사항이 바뀌었는지 (40자 이상)
updates_docs: [docs/design-docs/product-design.md]  # 이 결정이 바꾸는 문서. 같은 변경에서 실제로 고쳐야 한다 (DOC090)
# updates_docs: []      # 바꾸는 문서가 없으면 빈 목록과
# updates_docs_reason: "..."
amends:                 # 앞선 결정의 일부를 바꿀 때 (전체를 대체하면 아래 superseded)
  - adr: "0001"         #   그 ADR 번호
    what: 목표의 둘째 부분   #   고친 부분을 20자쯤의 쉬운 말로 (새 ADR은 꼭 적는다)
```

4. 앞선 결정의 일부를 바꾸면 `pnpm tsx harness/src/checks/docs.ts link-amends docs/design-docs/adr/NNNN-....md`로 대상 ADR의 앞부분에 `amended_by`를 남기고 같은 `what`을 옮겨 적는다. 결정 목록은 이것을 "일부 고침: 0001 (고친 부분)"으로 보인다. `what`은 대상 ADR의 어느 부분이 바뀌었는지를 말한다. 새 ADR이 `what` 없이 `amends`를 쓰면 `DOC091`이 실패하고, 옛 ADR의 파일 이름만 적은 항목은 그대로 읽는다. (`DOC091`은 양쪽이 맞는지도 본다.) 대상 ADR도 보호 경로이므로 이 ADR의 `touches_protected`에 대상 파일을 적는다.
5. 확정: `pnpm tsx harness/src/checks/docs.ts seal-adr docs/design-docs/adr/NNNN-....md`
6. 색인에는 줄을 더하지 않는다. [색인](../index.md)의 문서 목록은 하네스가 문서의 `title`에서 만들고(`docs.ts generate`), 결정 기록은 폴더를 가리킬 뿐이다. 결정 목록도 하네스가 만든다: `pnpm tsx harness/src/checks/docs.ts decisions`가 [결정 목록](../generated/decisions.md)을 쓴다. 이 파일들은 정리 작업이 main에서 새로 만들고, 변경은 커밋하지 않는다 (낡았으면 `DOC098`, `DOC102` 알림만 나오고, 손으로 고치면 `DOC099`로 실패한다, ADR-0002). [제품 설계 한 장](product-design.md)을 고쳐야 하는 결정이면 `updates_docs`에 적는다.

## 잠긴 Go 테스트를 바꿔야 할 때 (ADR-0023)

잠긴 단위는 Go에서는 최상위 테스트 함수 하나이고 키는 `go:<패키지 디렉터리>::<함수 이름>`이다(예: `go:backend/internal/worldgen::TestGoldenTinyWorld`). TypeScript 키(`파일 경로::시험 이름`)와 겹치지 않는다. 지우거나 이름을 바꾸거나 본문, 본문이 쓰는 도우미·표·상수·골든 값, `testdata/`의 파일이 바뀌면 그 키가 잠금을 깬다(`TLOCK001`, `TLOCK002`). 서식과 주석만 바뀐 것은 깨지 않는다.

1. 어느 키가 걸렸는지는 `pnpm check`가 알려 준다. 이름을 바꾸면 옛 키 하나(`TLOCK002`)와 새 키 하나(`TLOCK003`)가 걸리니 옛 키만 `amends_tests`에 적고, 새 키는 `pnpm lock-tests`가 더한다.
2. 새 ADR의 `amends_tests`에 걸린 키를 모두 적고 `test_change_reason`에 요구사항이 왜 바뀌었는지 40자 이상으로 적는다. 바뀌는 값이 있으면 옛 값과 새 값을 표로 적는다(ADR-0022처럼).
3. 확정한 뒤 `pnpm lock-tests --amend docs/design-docs/adr/NNNN-....md`로 잠금 파일의 지문을 새로 쓴다. 본문이 쓰는 도우미를 고쳤다면 그 도우미를 쓰는 모든 테스트가 걸리니 모두 적어야 한다.
4. 약화는 허용이 아니다: 요구사항이 바뀐 것이 아니라면 테스트가 아니라 코드를 고친다.
5. 작업 중에 만드는 임시 커밋에서는 새 테스트를 `pnpm lock-tests`로 잠그지 않는다. 테스트가 아직 바뀔 수 있어서, 잠근 뒤에 고치면 결정 기록으로 풀어야 한다. 테스트를 다 쓴 뒤 한 번만 잠근다.
6. 테스트가 기대는 비테스트 코드(`go.locked_support`, 예: 적합성 테스트의 사례표)를 고치면 그것을 부르는 테스트의 키가 걸린다. 사례를 더하거나 고치는 변경도 그 테스트의 키를 `amends_tests`에 적고, 사례가 줄지 않았고 단언이 약해지지 않았음을 `test_change_reason`에 적는다(ADR-0028).

## 확정된 ADR은 수정 금지

본문을 고치면 `DOC070`으로 실패한다. 결정을 바꾸려면 **새 ADR**을 쓰고
이전 ADR의 `status: superseded`, `superseded_by: NNNN-....md`를 설정한다. 일부만 바꾸면 `amends`와 `link-amends`로 양쪽에 남긴다. (front matter는 확정 대상이 아니다.)

## 결정은 강제 장치와 연결한다

`enforced_by`가 가리키는 규칙 코드나 테스트가 실제로 있어야 한다 (`DOC080`).
기계가 확인할 수 없는 결정은 `[none]`과 사유를 적는다. 그런 결정이 많아지면 방향을 잃은 것이다.

## 문서는 결정도 따라간다 (ADR-0002)

- 코드를 묘사하지 않는 문서는 `covers` 대신 `follows`(다른 문서나 ADR의 경로·글롭)를 둔다. 따라가는 문서의 본문이 바뀌면 `DOC010`으로 낡았다고 알린다 (`DOC092`는 둘 다 없는 문서).
- `follows`에는 그 문서가 틀리게 되는 원인이 되는 문서만 적는다(인용만 하는 곳은 적지 않는다). 따라가기는 목표 → 요구사항 → 설계 → 계획의 한 방향이고, 두 문서가 서로를 따라가게 두지 않는다 ([ADR-0002](adr/0002-harness-and-working-rules-from-terrarium.md)).
- 문서가 가리키는 것은 있어야 한다 (`DOC094`): `PRAXIUM_` 환경 변수, `pnpm` 명령, `METHOD /api/...` 길, 검사 코드, `파일.ts`의 `이름`, `ADR-NNNN`, `REQ-NNN`.
- 코드에서 셀 수 있는 숫자는 `<!-- fact:이름 -->숫자<!-- /fact -->` 모양으로 적으면 세어서 견준다 (`DOC095`). `reverify`로는 못 고친다. 숫자를 고친다.
- 테스트를 말하는 표는 `시험 파일` 칸에 테스트 경로를 적는다 (`DOC096`, 알림). 아직 없으면 '예정'.
- 확인 상태는 문서 앞부분에 없다(ADR-0002). 문서와 그것이 `covers`·`follows`하는 파일 하나마다 `docs/verified/` 아래에 확인 기록 파일 하나가 있고, 파일의 내용 지문과 날짜와 메모가 들어 있다. 글롭은 맞는 파일마다 대상 하나다. 대상 하나가 바뀌면 그 대상의 기록만 낡는다 (`DOC010`). `reverify <문서> --note "…"`는 낡은 대상 모두에 기록을 쓰고, `--target <경로>`는 하나만 쓴다. 두 변경이 서로 다른 대상에 대해 같은 문서를 다시 확인하면 다른 파일을 건드리니 충돌하지 않는다. 같은 대상이면 기록이 충돌하고, 그 충돌은 진짜다: 한쪽을 고른 뒤 다시 읽고 `reverify`.
- 본문이 그대로인 채 `reverify`만 된 문서는 기록의 `text_changed: false`로 알 수 있다. `reverify`는 같은 대상의 이전 기록과 같은 메모를 받지 않고, 한 번에 10개를 넘는 대상은 `--target`으로 나누게 한다(한 메모가 여러 대상을 덮는 도장을 막는다, ADR-0028). 구조 검사 요약에 알림(`DOC097`)으로 나오고, `docs.ts audit-queue`가 다른 눈이 읽을 차례를 보여 준다. 읽은 뒤 `docs.ts audit <문서> --note "한 줄"`(문서 폴더의 `_audit.yaml`에 날짜가 남는다).
- 옛 형식(앞부분에 `last_verified` 등)이 남아 있으면 `DOC100`이 한 번 실패하고 `pnpm tsx harness/src/checks/docs.ts migrate`를 알려 준다(여러 번 돌려도 같다). main을 합친 뒤 앞부분이 충돌하면 main의 앞부분을 고르고 `migrate`를 돌린다.
- 제품 목표 문장은 `requirements.md`의 "제품 목표" 아래 굵은 한 줄이 원본이다. README.md와 AGENTS.md 첫 단락에 그대로 있어야 한다 (`DOC093`).
