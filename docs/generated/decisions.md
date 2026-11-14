# 결정 목록

이 문서는 하네스가 만들어요(`pnpm tsx harness/src/checks/docs.ts decisions`). 손으로 수정하지 않아요. 결정 기록의 앞부분(제목, 상태, `amends`, `amended_by`, `superseded_by`)에서 번호 순으로 한 줄씩 만들고, 하루에 한 번 실행되는 정리 작업이 main에서 새로 만들어 올려요. 결정마다 커밋하지 않아요. 오래되면 구조 검사가 알림 한 줄만 내요.

'일부 수정'이 적힌 결정은 뒤의 결정이 그 일부를 바꿨다는 의미예요. 본문은 확정되어 그대로이니, 거기 적힌 결정 가운데 무엇이 지금 유효한지는 수정한 결정을 읽어 확인해요.

| 결정 | 상태 | 수정 내용 |
|---|---|---|
| [ADR-0001 프락시움은 테라리움에서 나온 운영 측 제품이다 — 목표는 초안으로 두고, 테라리움과는 파일 2개로만 연결한다](../design-docs/adr/0001-product-goal-draft-and-seams-with-terrarium.md) | 유효 | 일부 수정: [0004](../design-docs/adr/0004-goal-confirmed-and-handover-from-terrarium.md) (목표는 초안이 아니다), [0005](../design-docs/adr/0005-separate-product-own-model-verification-outside.md) (테라리움과의 연결, 같은 모델, 목표의 셋째 부분) |
| [ADR-0002 구조와 하네스는 테라리움의 것을 가져온다 — 기계가 강제하는 규칙, 짧은 지도, 스스로 오래됐음을 감지하는 문서](../design-docs/adr/0002-harness-and-working-rules-from-terrarium.md) | 유효 | 일부 수정: [0029](../design-docs/adr/0029-standard-terms-in-documents-and-screens.md) (화면 용어에 대한 작업 규칙을 용어집의 표준 용어로 변경하고 문서 본문에도 적용한다) |
| [ADR-0003 리포지토리에는 공개된 사실만 둔다 — 내부에서 들은 사실, 실제 데이터, 시크릿은 넣지 않는다](../design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md) | 유효 |  |
| [ADR-0004 제품 목표를 굳히고, 리포지토리는 공개로 두며, 테라리움에서 인계받은 것을 문서 1개로 둔다](../design-docs/adr/0004-goal-confirmed-and-handover-from-terrarium.md) | 유효 | 일부 수정: [0029](../design-docs/adr/0029-standard-terms-in-documents-and-screens.md) (목표 문장의 표기를 표준 용어로 수정하고 사내를 뺀다. 결정은 그대로다) |
| [ADR-0005 프락시움은 별도 제품이다 — 모델은 프락시움이 소유하고, 위임 전의 검증은 제품 밖에 둔다](../design-docs/adr/0005-separate-product-own-model-verification-outside.md) | 유효 |  |
| [ADR-0006 백엔드는 Go, 용도에 맞는 언어와 스토리지 — 클린 아키텍처, 역할 4종, 알파는 백엔드부터](../design-docs/adr/0006-go-backend-clean-architecture-backend-first.md) | 유효 |  |
| [ADR-0007 KG의 모델: 확장 가능한 타입과 고정된 링크 의미, 채널별 상태, 객체인 이중화 그룹, 근거 등급, 불변 ID, 충분한 표준 모델](../design-docs/adr/0007-kg-model.md) | 유효 |  |
| [ADR-0008 현실 데이터 수집: 어댑터와 프리셋, 수집 방식 4가지, 변경을 입력받아 변경을 출력하는 실행기 하나, 속성 단위로 객체를 구축하는 피드, 읽기 전용 3중 장치, 안전장치](../design-docs/adr/0008-ingestion.md) | 유효 |  |
| [ADR-0009 KG의 저장과 버전 — 진실은 PostgreSQL에, 불변 버전, 차이와 적용](../design-docs/adr/0009-kg-storage-and-versions.md) | 유효 | 일부 수정: [0025](../design-docs/adr/0025-kg-stands-on-a-sequence-of-changes-storage-port-and-reference-implementation.md) (링크의 내용 해시에 등급과 근거의 종류가 포함되고, 일부만 적용된 배치는 롤백하고 중단한다) |
| [ADR-0010 KG 질의 — 사전 정의 질의, 영향 범위의 의미, 프로젝션과 엔진](../design-docs/adr/0010-kg-queries-and-impact.md) | 유효 |  |
| [ADR-0011 KG를 운영한다: 문제는 규칙으로 해결하고, 질의는 중단되지 않고, 스트림이 중단돼도 잃지 않는다](../design-docs/adr/0011-kg-operations.md) | 유효 |  |
| [ADR-0012 권한: 사내 SSO의 사람과 그룹에, 제품 내장 역할을, 범위와 함께, 관리자가 부여한다](../design-docs/adr/0012-access-control.md) | 유효 |  |
| [ADR-0013 데이터 관측성: 실행기가 신호를 발행하고, 헬스 상태가 계보를 따라 전파되고, 사람과 에이전트가 함께 질의한다](../design-docs/adr/0013-data-observability.md) | 유효 |  |
| [ADR-0014 정의의 버전과 브랜치: 바뀌지 않는 정의 세트, 제안과 검사, 직렬 큐로 반영하는 병합, 앞으로 나아가는 리버트, 정의의 계보](../design-docs/adr/0014-definitions-branches-revert.md) | 유효 |  |
| [ADR-0015 역량 질문과 충족률: KG가 답해야 하는 질문을 정의로 적고, 객체마다 자동으로 계산해 비율로 낸다](../design-docs/adr/0015-competency-questions.md) | 유효 |  |
| [ADR-0016 화면은 다른 리포지토리에 두고, REST API의 계약이 유일한 경계다: 계약이 먼저, 메이저 버전, 일관된 형식](../design-docs/adr/0016-frontend-separate-rest-api-contract.md) | 유효 | 일부 수정: [0024](../design-docs/adr/0024-rest-contract-becomes-a-document-the-machine-keeps.md) (계약의 위치와 검사, 요구사항 REQ-143, REQ-148, REQ-229의 분리) |
| [ADR-0017 프락시움 자신의 관측성과 운영 가능성 — 에이전트가 관측하고, 진단하고, 정해진 운영 액션으로 운영할 수 있게 만든다](../design-docs/adr/0017-self-observability-and-operability.md) | 유효 |  |
| [ADR-0018 시크릿과 암호화 — 평문으로 두지 않고, 사용하는 시점에만 복호화하고, 다시 노출되지 않고, 사용 이력이 남는다](../design-docs/adr/0018-secrets-and-encryption.md) | 유효 |  |
| [ADR-0019 하네스가 Go 코드를 읽는다 — Go 백엔드의 위치와 검사](../design-docs/adr/0019-harness-reads-go-backend-place-and-checks.md) | 유효 | 일부 수정: [0023](../design-docs/adr/0023-harness-locks-go-tests-and-links-them-to-requirements.md) (Go 테스트 잠금과 추적과 등급표), [0024](../design-docs/adr/0024-rest-contract-becomes-a-document-the-machine-keeps.md) (코어 유닛 `core.kernel`이 운영체제의 `os`를 import한다(`entropy.go` 한 파일에서 환경 변수를 읽는 입구)), [0028](../design-docs/adr/0028-close-the-gaps-the-audit-found-in-the-checks.md) (구조 계약의 `go:` 절에 두는 필드와 import 규칙) |
| [ADR-0020 타입 등록부와 표준 모델의 데이터 파일을 도입한다](../design-docs/adr/0020-type-registry-and-standard-model-data.md) | 유효 |  |
| [ADR-0021 합성 세계 생성기를 core.worldgen 단위로 둔다](../design-docs/adr/0021-synthetic-world-generator.md) | 유효 |  |
| [ADR-0022 표준 모델을 영향 범위에 맞게 보강한다](../design-docs/adr/0022-standard-model-fitted-to-impact-scope.md) | 유효 |  |
| [ADR-0023 하네스가 Go 테스트를 잠그고 요구사항에 잇는다](../design-docs/adr/0023-harness-locks-go-tests-and-links-them-to-requirements.md) | 유효 | 일부 수정: [0028](../design-docs/adr/0028-close-the-gaps-the-audit-found-in-the-checks.md) (Go 테스트 규칙의 이식과 잠금의 지문 범위) |
| [ADR-0024 REST 계약이 기계가 지키는 문서가 된다](../design-docs/adr/0024-rest-contract-becomes-a-document-the-machine-keeps.md) | 유효 | 일부 수정: [0028](../design-docs/adr/0028-close-the-gaps-the-audit-found-in-the-checks.md) (`core.kernel`이 `os`를 import하는 범위) |
| [ADR-0025 KG를 변경 시퀀스 위에 구성한다 — 스토리지 포트와 기준 구현](../design-docs/adr/0025-kg-stands-on-a-sequence-of-changes-storage-port-and-reference-implementation.md) | 유효 | 일부 수정: [0028](../design-docs/adr/0028-close-the-gaps-the-audit-found-in-the-checks.md) (적합성 테스트 유닛이 기준 구현을 import하는 범위) |
| [ADR-0026 사전 정의 질의의 첫 3개가 기준 구현이 된다](../design-docs/adr/0026-closed-queries-first-three-stand-as-the-reference.md) | 유효 |  |
| [ADR-0027 KG를 API와 명령줄로 읽는다](../design-docs/adr/0027-reading-the-kg-through-the-api-and-the-command-line.md) | 유효 |  |
| [ADR-0028 감사에서 나온 검사의 허점을 보완한다](../design-docs/adr/0028-close-the-gaps-the-audit-found-in-the-checks.md) | 유효 |  |
| [ADR-0029 문서와 화면의 문구를 표준 용어로 통일하고, 옛 표기가 다시 쓰이지 않게 검사한다](../design-docs/adr/0029-standard-terms-in-documents-and-screens.md) | 유효 | 일부 수정: [0030](../design-docs/adr/0030-document-term-check-fails-on-old-spellings.md) (문서 용어 검사의 모드를 보고에서 실패로 전환한다) |
| [ADR-0030 문서 용어 검사를 실패 모드로 전환한다 — 정비를 마쳤으므로 옛 표기가 다시 쓰이면 병합할 수 없다](../design-docs/adr/0030-document-term-check-fails-on-old-spellings.md) | 유효 |  |
