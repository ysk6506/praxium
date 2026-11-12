# 결정 목록

이 문서는 하네스가 만들어요(`pnpm tsx harness/src/checks/docs.ts decisions`). 손으로 고치지 않아요. 결정 기록의 앞부분(제목, 상태, `amends`, `amended_by`, `superseded_by`)에서 번호 순으로 한 줄씩 만들고, 5시간마다 도는 정리 작업이 main에서 새로 만들어 올려요. 결정마다 커밋하지 않아요. 낡으면 구조 검사가 알림 한 줄만 내요.

'일부 고침'이 적힌 결정은 뒤의 결정이 그 일부를 바꿨다는 뜻이에요. 본문은 봉인되어 그대로이니, 거기 적힌 결정 가운데 무엇이 지금 유효한지는 고친 결정을 읽어 확인해요.

| 결정 | 상태 | 무엇이 고쳤나 |
|---|---|---|
| [ADR-0001 프락시움은 테라리움에서 나온 운영 쪽 제품이다 — 목표는 초안으로 두고, 테라리움과는 파일 둘로만 잇는다](../design-docs/adr/0001-product-goal-draft-and-seams-with-terrarium.md) | 유효 |  |
| [ADR-0002 구조와 하네스는 테라리움의 것을 가져온다 — 기계가 강제하는 규칙, 짧은 지도, 스스로 낡았음을 아는 문서](../design-docs/adr/0002-harness-and-working-rules-from-terrarium.md) | 유효 |  |
| [ADR-0003 저장소에는 공개된 사실만 둔다 — 내부에서 들은 사실, 실제 데이터, 비밀 값은 넣지 않는다](../design-docs/adr/0003-no-internal-facts-no-real-data-no-secrets.md) | 유효 |  |
