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
- [요구사항](product-specs/requirements.md) — 목표와 `REQ-NNN`의 단일 출처
- [제품 설계 한 장](design-docs/product-design.md) — 값, 원칙, 넘겨받은 숙제, 순서
- [테라리움에서 넘겨받은 것](references/from-terrarium.md) — 그때의 요구사항, 설계 결정, 풀리지 않은 쟁점 (출발 자료)
- [품질 등급표](generated/quality.md) — 단위별 등급 A~D (하네스가 만든 생성물)

## 문서 목록

<!-- docs-list:begin -->

### 제품 명세

- [프락시움 요구사항](product-specs/requirements.md)

### 설계

- [권한 설계 — 사내 SSO, 역할 여덟, 범위, 읽기와 가림, 승인, 감사](design-docs/access-control.md)
- [ADR 작성 절차](design-docs/adr-process.md)
- [역량 질문의 예시 — 패턴으로 끝까지 적은 지어낸 물음](design-docs/competency-examples.md)
- [역량 질문과 충족률 — KG가 답해야 하는 물음을 정의로 적고 객체마다 따진다](design-docs/competency-questions.md)
- [핵심 신념](design-docs/core-beliefs.md)
- [데이터 관측성 — 신호, 헬스 상태, 목표, 알림, 실행 소스 탐색](design-docs/data-observability.md)
- [기본값 표 — 제품이 싣는 숫자는 여기에 한 번만 적는다](design-docs/defaults.md)
- [에이전트가 정의를 쓸 때의 버전 관리 — 누가, 왜, 얼마나 빨리, 어디까지 저절로](design-docs/definitions-at-agent-scale.md)
- [정의의 계보 — 항목, 컬럼, 속성 단위로 따라가기와 "무엇이 영향받나"](design-docs/definitions-lineage.md)
- [정의의 버전과 브랜치 — 항목과 판, 정의 세트, 제안과 검사, 병합 큐, 리버트, 계보](design-docs/definitions.md)
- [용어집 — 화면과 문서에 쓰는 말은 여기서 정한다](design-docs/glossary.md)
- [품질 하네스 사용법](design-docs/harness-guide.md)
- [수집을 넓히는 길 — 푸시 수집, 지정 재수집, 기대와 대조, 매핑을 돕는 경로, 문서에서 줄까지](design-docs/ingestion-extensions.md)
- [현실 데이터 수집 설계 — 소스, 받는 방식, 수집, 원본 레코드, 읽기 전용, 안전장치](design-docs/ingestion.md)
- [KG의 모델 설계 — 타입 등록부, 링크 의미와 채널, 이중화 그룹, 근거, 식별](design-docs/kg-model.md)
- [KG 운영 — 문제 목록, 가용성, 동기화와 흐름의 실패 처리, 백업과 복구](design-docs/kg-operations.md)
- [영향 범위를 손으로 따라가기 — 가상의 예시 다섯](design-docs/kg-queries-examples.md)
- [KG에 묻기 — 사전 정의 질의, 영향 범위, 프로젝션과 엔진](design-docs/kg-queries.md)
- [KG 읽기의 측정 — 개발용 적재와 첫 셋 질의를 메모리 저장소에서 측정한 것](design-docs/kg-read-measurements.md)
- [KG의 적용 — 세 걸음, 큰 적용, 차례, 리스, 세대, 보관](design-docs/kg-storage-apply.md)
- [KG의 저장과 버전 — 무엇이 어디에 저장되고, 어떻게 봉해지는가](design-docs/kg-storage.md)
- [파이프라인 설계 — 변경을 받아 변경을 내는 실행기, 정제, 피드, 식별, 링크 정의](design-docs/pipeline.md)
- [저장소의 운영 수준 — PostgreSQL을 쓰기 중심으로 오픈 베타까지, 메시지 기록 저장소는 붙기만 한다](design-docs/postgres-operations.md)
- [E2E 프로세스의 방향 — 조사해서 잡은 방향이고 결정이 아니다](design-docs/process-direction.md)
- [제품 설계 한 장 — 프락시움의 값, 원칙, 쟁점의 답, 순서, 설계 문서의 지도](design-docs/product-design.md)
- [질의 엔진의 언어와 모양 — Go로 직접 만들고, 측정이 드러낸 것을 설계에 넣는다](design-docs/query-engine-shape.md)
- [REST API 기능마다의 경로 — 결정이 낸 기능에 `/v1`의 길이 있는가](design-docs/rest-api-feature-routes.md)
- [REST API의 자원과 경로 (3) — 목록과 해 보기, 에이전트 시대의 정의, 푸시 수집, 시크릿과 운영](design-docs/rest-api-resources-additions.md)
- [REST API 자원과 경로 (1) — 모델, 수집, 실행, 정의](design-docs/rest-api-resources-data.md)
- [REST API 자원과 경로 (2) — KG, 관측, 역량 질문, 권한, 바탕](design-docs/rest-api-resources-graph.md)
- [REST API 예시 셋 — 묻기, 정의 고치기, 보류된 스트림 풀기를 끝까지](design-docs/rest-api-walkthroughs.md)
- [REST API 설계 — 계약이 먼저, 일관된 형식, 로그인, 계약을 지키는 장치, CLI](design-docs/rest-api.md)
- [시크릿과 암호화 — 넣으면 다시 나오지 않고, 쓰는 자리에서만 풀리고, 쓴 기록이 남아요](design-docs/secrets-and-encryption.md)
- [프락시움 자신의 관측성과 운영 가능성 — 신호, 진단, 운영 액션, 권한과 기록](design-docs/self-observability.md)
- [표준 모델의 요약 — 층 아홉의 타입, 링크, 액션](design-docs/standard-model.md)
- [임시 콘솔 — 클로즈드 베타까지 기능을 눌러 보는, 삭제 쉬운 테스트용 화면](design-docs/temporary-console.md)
- [합성 세계 생성기 — 같은 시드이면 같은 데이터센터, 정답과 소스 보기와 변경 스트림과 분포 프로파일](design-docs/worldgen.md)

### 계획

- [지금의 순서 — KG 기반의 설계를 끝내고, 뼈대부터 끝까지 한 줄로 짓는다](exec-plans/active/plan.md)

### 그 밖의 문서

- [설계 조사 — 결정의 근거가 된 조사의 요약과 한계](references/design-research.md)
- [테라리움에서 넘겨받은 것 — 요구사항, 설계 결정, 풀리지 않은 쟁점](references/from-terrarium.md)
- [테라리움에 남아 있던 제품 책임자의 요구 — KG 기반의 출발 요구](references/owner-requirements-from-terrarium.md)

<!-- docs-list:end -->

## 결정 기록

결정 기록(ADR)은 [docs/design-docs/adr/](design-docs/adr/)에 있다. 손으로 쓰는 목록은 없다: 번호 순 목록과 어느 결정이 어느 결정을 고쳤는지는 하네스가 만든 [결정 목록](generated/decisions.md)에서 본다.

## 계획과 부채

- [지금의 순서](exec-plans/active/plan.md)
- [기술 부채 대장](exec-plans/tech-debt.yaml) (YAML)
