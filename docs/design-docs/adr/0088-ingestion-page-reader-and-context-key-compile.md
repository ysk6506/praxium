---
title: ADR-0088 수집의 페이지 읽기 포트와 문맥 키 컴파일 — 새 흐름의 DB 없는 앞부분
kind: decision
status: active
enforced_by:
  - TLOCK001
  - GOCOV001
  - backend/internal/ingest/pagestream_scale_test.go
  - backend/internal/ingest/scalecompile_scale_test.go
  - backend/internal/ingest/prepare_scale_test.go
  - backend/internal/ingest/intake_scale_test.go
  - backend/internal/filesource/pages_scale_test.go
touches_protected:
  - docs/product-specs/requirements-ingestion.md
  - harness/config/test-lock.json
updates_docs:
  - docs/guides/ingest-definitions.md
decision_hash: 1b771f5ed844092a
---
# ADR-0088 수집의 페이지 읽기 포트와 문맥 키 컴파일

## 맥락

ADR-0086이 수집을 페이지의 흐름으로 다시 설계했다. 이 결정은 그 구현의 첫 단계(I-2)가 `core.ingest`에 두는 DB 없는 앞부분이다: 페이지 읽기 포트, 파일 어댑터의 스트리밍, 읽은 행을 준비 행으로 바꾸는 변환, 페이지를 하나씩만 쥐는 수신 조정자, 준비 페이지를 받는 포트, 문맥 키의 컴파일. 옛 코드는 동작을 바꾸지 않고 옆에 둔다.

## 결정

1. **새 유닛이나 import는 없다.** 모두 기존 `core.ingest`와 `adapter.filesource` 안의 새 파일이다. 구조 계약은 바뀌지 않는다.
2. **정의 형식에 문맥 구성 요소를 더한다.** `identity.components`의 첫 항목은 `{context, parent}`일 수 있다(`KeyComponent.Context`, `KeyComponent.Parent`). 옛 컴파일(`Compile`)은 이를 받지 않고 거절하며, 새 컴파일(`CompileScale`)만 받는다. 새 오류 코드 7개는 `errcodes.go`가 아니라 `scalecompile.go`의 별도 표(`ScaleErrorCodes`)에 둔다. 옛 정의는 같은 계획이 나온다.
3. **요구사항의 표시.** REQ-639는 이 변경의 테스트가 모두 덮으므로 "예정" 표시와 단계 표기를 뗀다. REQ-655의 파일 어댑터 부분은 구현했지만 HTTP와 PostgreSQL 어댑터가 남아 있어 표시를 유지한다. REQ-633, 638, 652, 654, 656은 부분만 덮어서 이 변경의 테스트는 그 번호를 인용하지 않는다.
4. **이어 읽기 토큰은 아직 값이 없다.** 파일은 처음부터 읽으므로 페이지의 토큰은 비어 있고, `Resume` 옵션은 자리만 있다.

## 이유

- 앞부분만 DB 없이 먼저 구현하면 옛 경로와 같은 결과임을 같은 입력으로 비교해 확인할 수 있다.
- 옛 컴파일을 건드리지 않으면 옛 정의와 옛 흐름의 동작이 그대로다.

## 대안과 기각 사유

- **옛 `Compile`이 문맥을 받게 한다.** 옛 흐름은 문맥 키를 처리하지 못한다. 받고 나서 틀린 결과를 내는 것보다 거절이 낫다.
- **REQ-655의 표시를 뗀다.** HTTP와 PostgreSQL 어댑터가 같은 포트 위에 서지 않았다.

## 결과

- 잠긴 테스트는 새로 더해진 것만 있고 바뀐 것은 없다(`test-lock.json`은 추가만).
- xlsx와 JSON 배열 파일은 스트리밍하지 않는다(xlsx는 `unsupported`).
