---
title: 문서 색인
kind: reference
status: active
covers:
  - harness/src/checks/doc-index.ts
---
# 문서 색인

모든 문서는 여기서 도달 가능해야 한다 (`DOC040`). 아래 "문서 목록"은 하네스가 문서의 `title`에서 만든다: 새 문서를 만들어도 여기에 줄을 더하지 않는다. 정리 작업이 `pnpm tsx harness/src/checks/docs.ts generate`로 새로 만든다.

## 먼저 읽기

- [AGENTS.md](../AGENTS.md) — 에이전트 진입점, 절대 규칙
- [ARCHITECTURE.md](../ARCHITECTURE.md) — 단위와 의존 방향
- [요구사항](product-specs/requirements.md) — 목표(초안)와 `REQ-NNN`의 단일 출처
- [제품 설계 한 장](design-docs/product-design.md) — 값, 원칙, 넘겨받은 숙제, 순서
- [품질 등급표](generated/quality.md) — 단위별 등급 A~D (하네스가 만든 생성물)

## 문서 목록

<!-- docs-list:begin -->

### 제품 명세

- [프락시움 요구사항](product-specs/requirements.md)

### 설계

- [ADR 작성 절차](design-docs/adr-process.md)
- [핵심 신념](design-docs/core-beliefs.md)
- [용어집 — 화면과 문서에 쓰는 말은 여기서 정한다](design-docs/glossary.md)
- [품질 하네스 사용법](design-docs/harness-guide.md)
- [제품 설계 한 장 — 프락시움의 값, 원칙, 넘겨받은 숙제, 순서](design-docs/product-design.md)

### 계획

- [지금의 순서 — 구조를 세우고, 목표를 굳히고, 설계를 가져온다](exec-plans/active/plan.md)

<!-- docs-list:end -->

## 결정 기록

결정 기록(ADR)은 [docs/design-docs/adr/](design-docs/adr/)에 있다. 손으로 쓰는 목록은 없다: 번호 순 목록과 어느 결정이 어느 결정을 고쳤는지는 하네스가 만든 [결정 목록](generated/decisions.md)에서 본다.

## 계획과 부채

- [지금의 순서](exec-plans/active/plan.md)
- [기술 부채 장부](exec-plans/tech-debt.yaml) (YAML)
