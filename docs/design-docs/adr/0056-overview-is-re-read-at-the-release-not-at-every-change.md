---
title: ADR-0056 개요 문서의 재확인을 변경마다가 아니라 릴리스마다 한다 — follows의 재확인 시점(recheck)과 릴리스 검사
kind: decision
status: active
enforced_by:
  - DOC013
  - harness/src/checks/doc-release.test.ts
  - backend/cmd/praxium/release_plan_docs_test.go
updates_docs:
  - AGENTS.md
  - docs/design-docs/product-design.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/adr-process.md
  - docs/guides/release.md
  - docs/product-specs/requirements.md
touches_protected:
  - harness/**
  - .github/workflows/release.yml
  - AGENTS.md
  - docs/product-specs/requirements.md
  - docs/design-docs/adr/0002-harness-and-working-rules-from-terrarium.md
  - docs/design-docs/adr/0047-release-by-manual-run-with-version-and-commit.md
  - harness/config/test-lock.json
amends:
  - adr: "0002"
    what: 문서가 따르는 문서의 재확인 시점(변경마다, 릴리스마다)
  - adr: "0047"
    what: 릴리스를 만드는 실행의 plan 단계가 문서 확인을 한다
decision_hash: 76df65cbde723cce
---
# ADR-0056 개요 문서의 재확인을 변경마다가 아니라 릴리스마다 한다

## 맥락

제품 설계 개요(`product-design.md`)는 프런트매터의 `follows`로 요구사항 문서 전체를 따른다. 요구사항 문서가 한 글자라도 바뀌면 개요를 다시 확인했다는 기록(`docs/verified/…/docs__product-specs__requirements.md.yaml`)을 새로 써야 `DOC010`이 통과한다. 그런데 기능 변경은 거의 모두 요구사항을 더한다. 그래서 두 가지가 일어났다.

1. 모든 기능 PR이 **같은 기록 파일 하나**를 고친다. PR 하나가 병합될 때마다 나머지 PR이 그 파일에서 충돌하므로 병렬로 만든 PR을 한 줄로만 병합할 수 있다. 하루에 열 번 넘게 일어났다.
2. 재확인은 매번 "개요는 바뀌지 않아 그대로 둠" 한 줄을 기계적으로 적는 일이 됐다. 장치가 지키려던 것(요구사항이 바뀌면 개요가 낡지 않았는지 사람이 다시 읽는다)이 형식만 남았다.

요구사항을 더하는 변경은 개요의 어느 절에도 영향이 없는 경우가 대부분이다. 개요를 고쳐야 하는 변경은 드물고, 그때 필요한 것은 변경마다의 도장이 아니라 여러 변경이 쌓인 뒤에 한 번 하는 실제 대조다.

## 결정

강제 장치를 없애지 않고 **확인하는 시점을 옮긴다.** 따르는 관계마다 언제 다시 확인해야 하는지를 정할 수 있게 하고, 개요가 요구사항을 따르는 관계 하나만 릴리스마다 확인하게 한다.

**1. `follows` 항목의 재확인 시점 (REQ-394).** 항목은 문자열(지금까지의 형식)이거나 `{ doc: <경로나 글롭>, recheck: change | release }`이다.

```yaml
follows:
  - doc: docs/product-specs/requirements.md
    recheck: release
  - docs/design-docs/adr/0005-separate-product-own-model-verification-outside.md   # 문자열은 change
```

`change`(기본값)는 지금의 동작이다. 대상이 바뀌면 바로 `DOC010`으로 실패한다. `release`는 다음 릴리스 전에 다시 확인하면 된다. 문자열 항목은 `change`로 읽으므로 기존 문서는 하나도 고치지 않아도 같은 검사를 받는다. 같은 대상을 `change` 항목이나 `covers`도 가리키면 `change`가 이긴다. 모르는 `recheck` 값, `doc`이 없는 항목, 모르는 키는 프런트매터 오류(`DOC001`)다.

**2. `release` 관계의 낡은 기록 (REQ-395).** 확인 기록이 없거나 대상의 해시와 다르면 낡은 것이다.

- 보통 검사(`pnpm check`, `pnpm check:pr`, CI의 PR 검사)에서는 실패가 아니라 **알림**(`DOC103`)이다. 어느 문서가 어느 대상에 대해 낡았는지, 다음 릴리스 전에 다시 확인해야 한다는 것, 명령을 말한다. 종료 코드에 영향이 없다. 기능 PR은 이 기록을 건드리지 않는다.
- **릴리스 검사**에서는 `DOC013` 실패다. 어느 문서를 무엇에 대해 다시 확인해야 하는지와 명령(`docs.ts reverify <문서> --target <대상> --note "…"`)을 말한다. 릴리스 검사는 문서 검사 전체에 이 규칙을 더한 것이다. `change` 관계는 모드와 상관없이 지금과 같다.

**3. 릴리스 검사의 실행 (REQ-396).** (a) 검사 명령의 플래그 `docs.ts --release`. (b) 릴리스 워크플로의 `plan` 단계가 태그와 릴리스를 만들 때(`publish=true`: 태그 푸시와 버전을 넣은 수동 실행) `scripts/release-plan.sh`가 같은 명령을 실행한다. 낡은 `release` 관계가 있으면 `plan`이 실패하므로 빌드도 릴리스도 일어나지 않는다. 스냅숏은 아무것도 게시하지 않으므로 묻지 않는다. 이 검사는 **워크플로를 실행한 ref**의 문서를 본다. 수동 릴리스는 메인 브랜치의 끝이고, 태그 푸시는 태그가 가리키는 커밋이다. 빌드할 커밋(`commit` 입력)의 문서가 아니다. 릴리스 노트를 그 ref에서 읽는 것과 같은 이유로, 검토를 거쳐 병합된 것을 기준으로 삼기 위해서다. 그래서 `plan` 작업에 Node와 `pnpm install`이 필요해져 게시하는 실행에서만 이 단계를 더했다.

**4. 이번에 바꾸는 관계 (REQ-394).** `product-design.md`가 `requirements.md`를 따르는 관계 하나만 `release`로 바꾼다. 개요가 따르는 ADR-0005와 다른 문서의 관계는 그대로다. 요구사항을 고칠 때 함께 다시 써야 하는 `docs/verified/` 기록을 모두 찾아 보니 이것 하나뿐이다.

**5. 보완.** 릴리스 안내서(`docs/guides/release.md`)의 릴리스 절차 첫 단계에 "릴리스 전에 개요를 요구사항의 변경과 대조해 실제로 읽고, 고칠 곳이 있으면 고친 뒤 확인 기록을 쓴다"를 넣었다. 재확인 기록의 메모(`--note`)는 무엇과 대조했고 무엇을 고쳤는지, 또는 왜 고칠 것이 없는지를 적는 자리다. 실패 안내문도 같은 말을 한다.

## 이유

- 충돌의 원인은 "요구사항이 바뀔 때마다 같은 파일을 쓴다"는 것이다. 쓰는 시점을 릴리스로 옮기면 기능 PR은 이 파일을 건드리지 않는다.
- 변경마다 한 줄 도장을 찍는 확인은 확인이 아니다. 릴리스 전에 한 번, 쌓인 변경을 보며 읽는 쪽이 개요를 지키는 데 더 맞다.
- 강제 장치는 남는다. 낡은 기록은 보통 검사에서 계속 보이고(알림), 릴리스는 확인 없이는 나가지 못한다.

## 대안과 기각 사유

- **기록 파일을 없애거나 요구사항의 `follows`를 뺀다.** 요구사항이 바뀌어도 개요가 낡았는지 보는 곳이 아예 없어진다. 기각.
- **요구사항을 영역별 문서로 쪼갠다.** 개요가 따르는 것이 여러 개가 되어 기록도 여러 개가 되고, 같은 영역에 더하는 PR은 여전히 충돌한다. 기각.
- **병합 도구가 충돌을 자동으로 푼다.** 기록은 "읽었다"는 주장이므로 자동으로 합치면 확인이 아닌 것이 된다. 기각.
- **모든 `follows`를 릴리스 시점으로 바꾼다.** 이번에는 충돌이 증명된 관계 하나만 바꾼다. 다른 관계는 변경하는 PR이 문서를 고치는 것이 맞다.

## 결과

- **잃는 것.** 요구사항이 바뀐 직후에 개요가 낡았는지 보는 시점이 릴리스 직전으로 늦춰진다. 그 사이에 개요가 요구사항과 어긋난 채 머물 수 있고, 알림만이 그것을 상기시킨다.
- **지키는 것.** 릴리스는 확인 없이는 나가지 못한다. 확인 기록은 여전히 대상의 해시를 담으므로 확인 뒤에 요구사항이 다시 바뀌면 다음 릴리스 전에 다시 낡은 것이 된다. `change` 관계의 동작은 그대로다.
- **알려진 한계.** 릴리스 검사는 문서 검사 전체이므로, 시간이 지나 낡은 문서(`DOC020`)가 있으면 그것도 릴리스를 막는다. `harness/config/docs.yaml`이 없는 체크아웃(기존 릴리스 스크립트 테스트의 가짜 저장소)은 문서가 없으므로 `release-plan.sh`가 확인을 건너뛴다.
- **되돌리는 방법.** `product-design.md`의 항목을 문자열로 되돌리면(`- docs/product-specs/requirements.md`) 그 관계는 다시 변경마다의 확인이 된다. `recheck`를 어느 문서도 쓰지 않게 되면 `release-plan.sh`의 확인 단계와 워크플로의 설치 단계를 뺀다. 이 결정이 더한 테스트는 지금의 동작이 아니라 새 기능을 확인하므로 그대로 둔다.
