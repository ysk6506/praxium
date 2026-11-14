---
title: 문서 색인
kind: reference
status: active
covers:
  - harness/src/checks/doc-index.ts
---
# 문서 색인

모든 문서는 여기서 도달 가능해야 한다 (`DOC040`). 아래 "문서 목록"은 하네스가 문서의 `title`에서 생성한다: 새 문서를 만들어도 여기에 줄을 더하지 않는다. 정리 작업이 `pnpm tsx harness/src/checks/docs.ts generate`로 다시 생성한다.

## 먼저 읽기

- [AGENTS.md](../AGENTS.md) — 에이전트 진입점, 절대 규칙
- [ARCHITECTURE.md](../ARCHITECTURE.md) — 유닛과 의존 방향
- [요구사항](product-specs/requirements.md) — 목표와 `REQ-NNN`의 단일 출처
- [제품 설계 한 장](design-docs/product-design.md) — 값, 원칙, 넘겨받은 과제, 순서
- [테라리움에서 넘겨받은 것](references/from-terrarium.md) — 그때의 요구사항, 설계 결정, 해결되지 않은 쟁점 (출발 자료)
- [품질 등급표](generated/quality.md) — 유닛별 등급 A~D (하네스가 생성한 문서)

## 문서 목록

<!-- docs-list:begin -->

### 제품 명세

- [프락시움 요구사항](product-specs/requirements.md)

### 설계

- [권한 설계 — 사내 SSO, 역할 8종, 범위, 읽기와 가림, 승인, 감사](design-docs/access-control.md)
- [ADR 작성 절차](design-docs/adr-process.md)
- [역량 질문의 예시 — 패턴으로 끝까지 작성한 가상의 질문](design-docs/competency-examples.md)
- [역량 질문과 충족률 — KG가 답해야 하는 질문을 정의로 적고 객체별로 평가한다](design-docs/competency-questions.md)
- [핵심 신념](design-docs/core-beliefs.md)
- [데이터 관측성 — 신호, 헬스 상태, 목표, 알림, 실행 소스 탐색](design-docs/data-observability.md)
- [기본값 표 — 제품에 포함된 숫자는 여기에 한 번만 적는다](design-docs/defaults.md)
- [에이전트가 정의를 작성할 때의 버전 관리 — 누가, 왜, 얼마나 빠르게, 어디까지 자동으로](design-docs/definitions-at-agent-scale.md)
- [정의의 계보 — 항목, 컬럼, 속성 단위의 추적과 영향 분석](design-docs/definitions-lineage.md)
- [정의의 버전과 브랜치 — 항목과 리비전, 정의 세트, 제안과 검사, 병합 큐, 리버트, 계보](design-docs/definitions.md)
- [일반 용어집 — 개발과 운영에서 쓰는 표준 용어](design-docs/glossary-general.md)
- [용어집 — 문서와 화면에 쓰는 표준 용어](design-docs/glossary.md)
- [품질 하네스 사용법](design-docs/harness-guide.md)
- [수집 확장 — 푸시 수집, 지정 재수집, 기대와 대조, 매핑 지원 API, 문서에서 행까지](design-docs/ingestion-extensions.md)
- [현실 데이터 수집 설계 — 소스, 수집 방식, 수집, 원본 레코드, 읽기 전용, 안전장치](design-docs/ingestion.md)
- [KG의 모델 설계 — 타입 등록부, 링크 의미와 채널, 이중화 그룹, 근거, 식별](design-docs/kg-model.md)
- [KG 운영 — 문제 목록, 가용성, 동기화와 스트림의 실패 처리, 백업과 복구](design-docs/kg-operations.md)
- [영향 범위를 손으로 따라가기 — 가상의 예시 5개](design-docs/kg-queries-examples.md)
- [KG 질의 — 사전 정의 질의, 영향 범위, 프로젝션과 엔진](design-docs/kg-queries.md)
- [KG 읽기 성능 측정 — 개발용 적재와 첫 3개 질의를 메모리 스토리지에서 측정한 결과](design-docs/kg-read-measurements.md)
- [KG의 적용 — 3단계, 큰 적용, 순서, 리스, 세대, 보존](design-docs/kg-storage-apply.md)
- [KG의 저장과 버전 — 무엇이 어디에 저장되고, 어떻게 확정되는가](design-docs/kg-storage.md)
- [파이프라인 설계 — 변경을 입력받아 변경을 산출하는 실행기, 정제, 피드, 식별, 링크 정의](design-docs/pipeline.md)
- [스토리지의 운영 수준 — PostgreSQL을 쓰기 중심으로 오픈 베타까지, 메시지 로그 스토리지는 붙기만 한다](design-docs/postgres-operations.md)
- [엔드투엔드(E2E) 프로세스의 방향 — 조사로 정리한 방향이고 결정이 아니다](design-docs/process-direction.md)
- [제품 설계 개요 — 프락시움의 가치, 원칙, 쟁점별 답, 순서, 설계 문서 지도](design-docs/product-design.md)
- [질의 엔진의 언어와 구조 — Go로 직접 구현하고, 측정이 드러낸 것을 설계에 반영한다](design-docs/query-engine-shape.md)
- [REST API 기능별 경로 — 결정이 정한 기능마다 `/v1` 경로가 있는가](design-docs/rest-api-feature-routes.md)
- [REST API의 자원과 경로 (3) — 카탈로그와 사전 검증, 에이전트 시대의 정의, 푸시 수집, 시크릿과 운영](design-docs/rest-api-resources-additions.md)
- [REST API 자원과 경로 (1) — 모델, 수집, 실행, 정의](design-docs/rest-api-resources-data.md)
- [REST API 자원과 경로 (2) — KG, 관측, 역량 질문, 권한, 기반](design-docs/rest-api-resources-graph.md)
- [REST API 예시 3가지 — 질의, 정의 수정, 보류된 스트림 해제를 처음부터 끝까지](design-docs/rest-api-walkthroughs.md)
- [REST API 설계 — 계약이 먼저, 일관된 형식, 로그인, 계약을 지키는 장치, CLI](design-docs/rest-api.md)
- [시크릿과 암호화 — 입력 후 재조회 불가, 사용 시점에만 복호화, 사용 이력 기록](design-docs/secrets-and-encryption.md)
- [프락시움 자신의 관측성과 운영 가능성 — 신호, 진단, 운영 액션, 권한과 기록](design-docs/self-observability.md)
- [표준 모델의 요약 — 9개 층의 타입, 링크, 액션](design-docs/standard-model.md)
- [임시 콘솔 — 클로즈드 베타까지 기능을 눌러 보는, 삭제 쉬운 테스트용 화면](design-docs/temporary-console.md)
- [합성 세계 생성기 — 같은 시드이면 같은 데이터센터, 정답과 소스 보기와 변경 스트림과 분포 프로파일](design-docs/worldgen.md)

### 계획

- [지금의 순서 — KG 기반 설계를 끝내고, 뼈대부터 끝까지 수직 슬라이스로 구축한다](exec-plans/active/plan.md)

### 그 밖의 문서

- [설계 조사 — 결정의 근거가 된 조사의 요약과 한계](references/design-research.md)
- [테라리움에서 넘겨받은 것 — 요구사항, 설계 결정, 풀리지 않은 쟁점](references/from-terrarium.md)
- [테라리움에 남아 있던 제품 책임자의 요구 — KG 기반의 출발 요구](references/owner-requirements-from-terrarium.md)

<!-- docs-list:end -->

## 결정 기록

결정 기록(ADR)은 [docs/design-docs/adr/](design-docs/adr/)에 있다. 손으로 쓰는 목록은 없다: 번호 순 목록과 어느 결정이 어느 결정을 수정했는지는 하네스가 생성한 [결정 목록](generated/decisions.md)에서 본다.

## 계획과 부채

- [지금의 순서](exec-plans/active/plan.md)
- [기술 부채 대장](exec-plans/tech-debt.yaml) (YAML)
