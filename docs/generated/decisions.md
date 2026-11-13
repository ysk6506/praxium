# 결정 목록

이 문서는 하네스가 만들어요(`pnpm tsx harness/src/checks/docs.ts decisions`). 손으로 고치지 않아요. 결정 기록의 앞부분(제목, 상태, `amends`, `amended_by`, `superseded_by`)에서 번호 순으로 한 줄씩 만들고, 5시간마다 도는 정리 작업이 main에서 새로 만들어 올려요. 결정마다 커밋하지 않아요. 낡으면 구조 검사가 알림 한 줄만 내요.

'일부 고침'이 적힌 결정은 뒤의 결정이 그 일부를 바꿨다는 뜻이에요. 본문은 봉인되어 그대로이니, 거기 적힌 결정 가운데 무엇이 지금 유효한지는 고친 결정을 읽어 확인해요.

| 결정 | 상태 | 무엇이 고쳤나 |
|---|---|---|
| [ADR-0001 프락시움은 테라리움에서 나온 운영 쪽 제품이다 — 목표는 초안으로 두고, 테라리움과는 파일 둘로만 잇는다](../design-docs/adr/0001-product-goal-draft-and-seams-with-terrarium.md) | 유효 | 일부 고침: [0004](../design-docs/adr/0004-goal-confirmed-and-handover-from-terrarium.md) (목표는 초안이 아니다), [0005](../design-docs/adr/0005-separate-product-own-model-verification-outside.md) (테라리움과 잇기, 같은 모델, 목표 셋째 부분) |
| [ADR-0002 구조와 하네스는 테라리움의 것을 가져온다 — 기계가 강제하는 규칙, 짧은 지도, 스스로 낡았음을 아는 문서](../design-docs/adr/0002-harness-and-working-rules-from-terrarium.md) | 유효 |  |
| [ADR-0003 저장소에는 공개된 사실만 둔다 — 내부에서 들은 사실, 실제 데이터, 비밀 값은 넣지 않는다](../design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md) | 유효 |  |
| [ADR-0004 제품 목표를 굳히고, 저장소는 공개로 두며, 테라리움에서 넘겨받은 것을 한 장으로 둔다](../design-docs/adr/0004-goal-confirmed-and-handover-from-terrarium.md) | 유효 |  |
| [ADR-0005 프락시움은 별도 제품이다 — 모델은 프락시움이 쥐고, 맡기기 전의 검증은 제품 밖에 둔다](../design-docs/adr/0005-separate-product-own-model-verification-outside.md) | 유효 |  |
| [ADR-0006 백엔드는 Go, 자리에 맞는 언어와 저장소 — 클린 아키텍처, 역할 넷, 알파는 백엔드부터](../design-docs/adr/0006-go-backend-clean-architecture-backend-first.md) | 유효 |  |
| [ADR-0007 KG의 모델: 열린 타입과 닫힌 링크의 뜻, 채널마다의 상태, 객체인 이중화 묶음, 근거 등급, 바뀌지 않는 ID, 넉넉한 표준 모델](../design-docs/adr/0007-kg-model.md) | 유효 |  |
| [ADR-0008 현실 데이터 받기: 어댑터와 프리셋, 받는 방식 넷, 변경을 받아 변경을 내는 실행기 하나, 속성 단위로 객체를 짓는 공급, 읽기 전용 세 겹, 안전장치](../design-docs/adr/0008-ingestion.md) | 유효 |  |
| [ADR-0009 KG의 저장과 버전 — 진실은 PostgreSQL에, 바뀌지 않는 버전, 차이와 적용](../design-docs/adr/0009-kg-storage-and-versions.md) | 유효 |  |
| [ADR-0010 KG에 묻기 — 닫힌 질의, 영향 범위의 뜻, 투영과 엔진](../design-docs/adr/0010-kg-queries-and-impact.md) | 유효 |  |
| [ADR-0011 KG를 운영한다: 문제는 규칙으로 풀고, 묻기는 멈추지 않고, 흐름이 멈춰도 잃지 않는다](../design-docs/adr/0011-kg-operations.md) | 유효 |  |
| [ADR-0012 권한: 사내 SSO의 사람과 무리에, 제품에 든 역할을, 범위와 함께, 관리자가 준다](../design-docs/adr/0012-access-control.md) | 유효 |  |
| [ADR-0013 데이터 관측성: 실행기가 신호를 내고, 건강이 계보를 따라 흐르고, 사람과 에이전트가 함께 묻는다](../design-docs/adr/0013-data-observability.md) | 유효 |  |
| [ADR-0014 정의의 버전과 브랜치: 바뀌지 않는 정의 묶음, 제안과 검사, 한 줄로 들이는 병합, 앞으로 나아가는 되돌리기, 정의의 계보](../design-docs/adr/0014-definitions-branches-revert.md) | 유효 |  |
| [ADR-0015 역량 질문과 충족률: KG가 답해야 하는 물음을 정의로 적고, 객체마다 저절로 따져 비율로 낸다](../design-docs/adr/0015-competency-questions.md) | 유효 |  |
| [ADR-0016 화면은 다른 저장소에 두고, REST API의 계약이 유일한 이음매다: 계약이 먼저, 큰 버전, 한결같은 모양](../design-docs/adr/0016-frontend-separate-rest-api-contract.md) | 유효 |  |
| [ADR-0017 프락시움 자신의 관측성과 운영 가능성 — 에이전트가 볼 수 있고, 진단할 수 있고, 정해진 행위로 돌볼 수 있게 만든다](../design-docs/adr/0017-self-observability-and-operability.md) | 유효 |  |
| [ADR-0018 비밀 값과 암호화 — 평문으로 두지 않고, 쓰는 자리에서만 풀고, 다시 나오지 않고, 쓴 기록이 남는다](../design-docs/adr/0018-secrets-and-encryption.md) | 유효 |  |
| [ADR-0019 하네스가 Go 코드를 읽는다 — Go 백엔드의 자리와 검사](../design-docs/adr/0019-harness-reads-go-backend-place-and-checks.md) | 유효 |  |
| [ADR-0020 타입 등록부와 표준 모델의 데이터 파일이 선다](../design-docs/adr/0020-type-registry-and-standard-model-data.md) | 유효 |  |
