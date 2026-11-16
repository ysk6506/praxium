---
title: ADR-0069 화면은 이 저장소에서 만들고, 교체하기 쉽게 지킨다
kind: decision
status: active
enforced_by:
  - ARCH010
  - ARCH011
  - ARCH020
  - WEB001
  - WEB010
  - WEB011
  - WEB012
  - WEB013
  - WEB020
  - WEB021
  - WEB030
  - WEB031
  - WEB032
  - WEB042
updates_docs:
  - AGENTS.md
  - ARCHITECTURE.md
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-web-cli-release.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements-operations.md
  - docs/design-docs/temporary-console.md
  - docs/design-docs/web-ui-rules.md
  - docs/references/owner-requirements-from-terrarium.md
  - docs/design-docs/glossary.md
  - docs/design-docs/product-design.md
  - docs/design-docs/defaults.md
  - docs/design-docs/definitions.md
  - docs/design-docs/pipeline.md
  - docs/design-docs/rest-api.md
  - docs/design-docs/rest-api-feature-routes.md
  - docs/design-docs/rest-api-resources-additions.md
  - docs/design-docs/competency-questions.md
  - docs/design-docs/data-observability.md
  - docs/design-docs/kg-model.md
  - docs/design-docs/kg-operations.md
  - docs/design-docs/kg-queries.md
  - docs/design-docs/secrets-and-encryption.md
  - docs/design-docs/self-observability.md
  - docs/exec-plans/active/plan.md
  - docs/exec-plans/tech-debt.yaml
touches_protected:
  - AGENTS.md
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-web-cli-release.md
  - docs/product-specs/requirements-query-api.md
  - docs/product-specs/requirements-operations.md
  - docs/design-docs/adr/0016-frontend-separate-rest-api-contract.md
  - docs/design-docs/adr/0035-web-ui-in-apps-web-embedded-in-the-program.md
amends:
  - adr: "0016"
    what: 1번과 9번(화면 코드의 위치, 콘솔의 성격과 삭제 조건, 기본 꺼짐)
  - adr: "0035"
    what: 1번의 임시 콘솔이라는 성격과 지우는 조건
decision_hash: e880daffb7ef2837
---
# ADR-0069 화면은 이 저장소에서 만들고, 교체하기 쉽게 지킨다

## 맥락

ADR-0016은 화면을 다른 리포지토리(아직 없다)의 일로 정하고, 이 리포지토리에는 지울 수 있는 임시 콘솔만 두었다(9번). ADR-0035가 그 콘솔을 `apps/web`의 React 앱으로 키웠지만 "임시이고 프런트엔드 리포지토리가 서거나 클로즈드 베타가 끝나면 지운다(TD-0007)"는 조건은 그대로였다.

제품 책임자가 이 방향을 바꿨다. 화면과 계약이 함께 바뀌는 시기에는 리포지토리 둘을 맞추는 비용이 크고, 화면이 없는 저장소에서는 제품을 써 보기 어렵다. 한편 제품을 쓰는 조직은 저장소를 가져가 자기 디자인 시스템으로 화면을 교체하거나 화면을 통째로 다시 만들고 싶을 수 있다. 백엔드는 그대로 두고 화면만 바꿀 수 있어야 한다.

## 결정

1. **화면의 코드는 `apps/web`에 둔다.** 따로 된 프런트엔드 리포지토리는 두지 않는다. ADR-0016 1번의 "화면의 코드는 두지 않는다"와 9번의 예외(임시, 삭제 조건)는 이 결정이 대신한다.
2. **화면을 교체하기 쉽게 지킨다.** 지금 "삭제할 수 있게 지키는 규칙"이던 것을 이 이름으로 이어받는다.
   - 화면은 공개된 `/v1`만 부른다. 화면 전용 경로를 만들지 않는다. 화면이 할 수 있는 일은 모두 `/v1`로 된다(ADR-0016 7번과 10번).
   - 규칙을 화면이 복제하지 않는다. 규칙의 목록은 API가 준다(`/v1/catalog/...`).
   - 화면의 계층은 위로만 의존한다(`api` ← `data` ← `screens`, `ui` ← `screens`). 색, 글꼴, 간격의 토큰은 한 곳(`apps/web/src/ui/tokens.css`)에 둔다. 디자인 시스템을 바꾸는 일이 `ui`와 토큰의 일이 되게 하려는 것이다.
   - 백엔드의 Go 코드는 `apps/web`을 가리키지 않는다. 화면 없이도(`-web=false`, 화면이 없는 빌드) 제품의 모든 기능이 API와 CLI로 된다.
3. **화면은 쓰기를 할 수 있다.** 무엇을 쓰는지는 그 기능의 결정 기록이 정한다. 이 결정은 허용하는 데까지만 적는다(소스와 매핑을 편집하는 화면은 ADR-0058이 정한다. 예정).
4. **지금 화면의 성격.** 지금의 화면은 기능을 확인할 수 있는 수준이고, 화면의 설계(UX와 UI)는 클로즈드 베타에서 한다. "임시 콘솔"이라는 이름과 "지운다"는 조건은 없앤다(TD-0007과 기한도 닫는다). 문서에서는 "웹 화면" 또는 "콘솔"로 쓴다. 파일 `temporary-console.md`의 이름은 결정 기록과 확인 기록이 가리키므로 두고 내용만 고친다.
5. **`-web`의 기본값은 코드대로 켜짐이다.** 끄려면 `-web=false`를 준다. ADR-0016 9번의 "기본은 꺼져 있다"는 문장은 이 결정이 고친다. 프로덕션 모드에서 화면을 낼지는 제품 책임자가 정한다(ADR-0035의 결과 그대로).
6. **그대로 두는 것.**
   - 화면의 테스트가 잠금과 요구사항 추적에서 빠지는 규칙(ADR-0035 7번). 화면의 논리는 `data` 계층에 두어 잠기게 한다.
   - 의존성의 한도와 번들 예산(ADR-0035, ADR-0045). 예산을 재는 법은 뒤의 결정 기록이 다룬다.
   - [웹 UI 규칙](../web-ui-rules.md).
7. **다른 결정 기록의 "화면은 다른 리포지토리" 문장.** 확정된 본문이라 고치지 않는다. ADR-0016을 가리키는 그 문장들은 이 결정으로 함께 바뀐다.

## 이유

- 계약, 생성 코드, 검사가 한 저장소에 있으면 계약의 변경이 화면의 타입 오류로 바로 드러난다. 두 저장소의 버전을 맞추는 일이 없다.
- 화면이 같은 저장소에 있어야 저장소만 받은 사람이 제품을 바로 써 볼 수 있다. 데모와 로컬 모드가 그 증거다(ADR-0038, ADR-0041).
- 교체하기 쉬운 구조는 저장소를 나누지 않고도 지킬 수 있다. 계층과 토큰은 검사가 지키고, 경계(`/v1`)는 백엔드의 계약과 같아서 화면의 코드만 바꾸면 된다.
- 화면이 쓰기를 할 수 있어야 소스와 매핑을 고치는 일을 사람이 화면에서 할 수 있다. 쓰기도 `/v1`로 되므로 CLI와 에이전트가 같은 일을 한다.

## 버린 대안

- **따로 된 프런트엔드 리포지토리(지금까지의 결정).** 화면과 계약이 함께 바뀌는 시기에 리포지토리 둘을 맞추는 비용이 크고, 화면 없이 제품을 써 볼 수 없다.
- **화면을 백엔드에 묶어 서버가 그리는 방식.** 화면 처리가 백엔드로 스며들어 화면을 교체하기 어렵다.
- **임시 콘솔을 그대로 두고 지우는 날을 미루기.** 지울 계획이 없는 코드에 "임시"라는 이름을 붙이면 문서가 사실과 달라진다.

## 결과

- AGENTS.md, ARCHITECTURE.md, 요구사항(REQ-209, REQ-242, 입구의 범위 밖과 REQ-002, REQ-003의 메모, REQ-240), 콘솔 설계 문서, 용어집, 개요와 설계 문서의 "화면은 다른 리포지토리" 문장, 계획을 고친다.
- TD-0007은 이 결정으로 사라지는 부채라 `wontfix`로 닫는다. TD-0006의 옮길 곳은 프런트엔드 리포지토리에서 `apps/web`으로 바뀐다.
- 규칙, 한도, 설정, 테스트는 바뀌지 않는다.
- 관련: ADR-0016, ADR-0035, ADR-0045, ADR-0038, ADR-0041.
