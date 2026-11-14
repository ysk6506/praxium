---
title: ADR-0063 요구사항 문서를 입구와 영역별 문서로 나눈다 — 번호는 전체에서 하나뿐
kind: decision
status: active
enforced_by:
  - TRACE004
  - harness/src/checks/requirement-files.test.ts
updates_docs:
  - AGENTS.md
  - docs/index.md
  - docs/design-docs/adr-process.md
  - docs/design-docs/harness-guide.md
  - docs/design-docs/product-design.md
  - docs/guides/release.md
  - docs/product-specs/requirements.md
touches_protected:
  - harness/**
  - .github/CODEOWNERS
  - AGENTS.md
  - docs/product-specs/requirements.md
  - docs/product-specs/requirements-*.md
  - docs/design-docs/adr/0023-harness-locks-go-tests-and-links-them-to-requirements.md
  - harness/config/test-lock.json
amends:
  - adr: "0023"
    what: 요구사항의 단일 출처가 한 파일에서 입구와 영역별 문서로
decision_hash: 08713b5fffdb54b9
---
# ADR-0063 요구사항 문서를 입구와 영역별 문서로 나눈다

## 맥락

`docs/product-specs/requirements.md`는 모든 요구사항을 한 파일에 가졌고 400줄 한도(`DOC050`)에 정확히 닿았다. 요구사항을 한 줄 더하는 다음 PR부터 `DOC050`으로 실패한다. 한도를 올리면 문서가 읽기 어려워지는 것을 막는 규칙의 뜻이 없어진다. 한 문서에 200번대 절 하나가 157줄이어서, 주제가 다른 요구사항이 번호 순서로 섞여 있기도 했다.

## 결정

1. **구조 (REQ-397).** 요구사항은 입구 문서 `docs/product-specs/requirements.md`(제품 목표, 기반, 프로세스, 범위 밖, 영역별 문서의 목록)와 영역별 문서 `docs/product-specs/requirements-<영역>.md`에 있다. 영역은 모델과 저장, 질의와 API, 영향 범위, 수집, 수집의 사용성과 메시지, 정의, 인증·권한·시크릿, 운영과 관측, 웹 UI·명령줄·릴리스의 아홉이다. 영역은 번호가 아니라 내용으로 정했고, 어느 파일도 250줄을 넘지 않는다(앞으로 늘어날 자리). 새 요구사항은 내용이 맞는 영역 문서에 더하고, 영역 문서가 250줄 가까이 되면 주제별로 나눈다.
2. **옮기기만 했다.** 요구사항의 줄은 글자 하나 바꾸지 않고 옮겼다. 옮기기 전과 뒤의 `REQ` 줄 전체와 번호 집합이 같다는 것을 스크립트로 대조했다(번호 285개, 새로 더한 REQ-397 하나만 다르다).
3. **번호는 전체에서 하나뿐이다.** 추적 검사(`traceability.ts`)와 문서의 `REQ-NNN` 참조 검사(`doc-claims.ts`)는 입구 문서와 `requirements-*.md`(소문자, 숫자, 하이픈 이름)를 모두 읽는다. 같은 번호가 두 파일에 있으면 `TRACE004`로 실패하고 뒤의 파일을 가리킨다. 테스트는 영역 문서에만 있는 번호를 가리킬 수 있다. 목표 문장의 출처(`goal_source`)는 입구 문서 그대로다. 두 검사가 함께 쓰는 파일 목록은 `requirement-files.ts`에 있다.
4. **보호.** `docs/product-specs/requirements-*.md`를 보호 경로(`protected-paths.txt`)와 코드 소유자(`CODEOWNERS`)에 더한다. 입구 문서만 보호하면 영역 문서가 보호 없이 바뀌어 요구사항 변경에 ADR이 필요하다는 규칙이 빠져나가기 때문이다. 다른 규칙, 한도, 설정은 바꾸지 않는다.
5. **관계.** `product-design.md`의 `follows`는 `docs/product-specs/requirements*.md`(`recheck: release`, ADR-0056)가 입구와 영역 문서를 모두 잡는다. 영역 문서는 ADR-0005를 따른다.

## 이유

- 한도는 문서를 나누라는 뜻이다. 줄이는 방향이 맞고 올리는 방향이 아니다.
- 영역별로 나누면 요구사항을 보태는 PR이 서로 다른 파일을 고쳐 텍스트 충돌도 줄어든다.
- 번호의 단일성은 검사가 지키므로 문서가 여럿이어도 `REQ-NNN`은 여전히 하나의 이름 공간이다.

## 대안과 기각 사유

- **한도를 올린다.** 규칙을 통과시키려고 규칙을 바꾸는 것이다. 기각.
- **폴더로 나누고 영역마다 하위 폴더를 둔다.** 파일이 아홉이라 폴더는 과하고 글롭(`requirements*.md`)이 단순하다. 기각.
- **영역 문서마다 번호 범위를 따로 둔다.** 번호는 이미 내용과 어긋나게 쓰였다(200번대에 여러 주제). 범위를 강제하려면 옮겨 번호를 바꿔야 하고 테스트와 ADR의 참조가 깨진다. 기각.

## 결과

- 요구사항의 문장과 번호는 그대로이므로 테스트의 `[REQ-NNN]`, ADR의 참조, 잠금 파일은 바뀌지 않는다.
- 영역 문서 아홉이 새로 생겨 개요의 확인 대상(`release` 관계)이 입구를 포함해 열 개가 된다. 릴리스 전에 바뀐 영역 문서만 `--target`으로 다시 확인한다.
- 되돌리는 방법: 영역 문서의 줄을 입구 문서로 합치면 된다(줄 수 한도가 허락할 때). 검사는 영역 문서가 없어도 입구 문서 하나만으로 그대로 동작한다.
