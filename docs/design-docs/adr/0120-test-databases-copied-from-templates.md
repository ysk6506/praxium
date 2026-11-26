---
title: ADR-0120 테스트 데이터베이스를 템플릿 데이터베이스의 복제로 준비하고 테스트를 나란히 돌린다
kind: decision
status: active
enforced_by:
  - TLOCK001
  - backend/internal/pgconn/tpl_test.go
  - backend/internal/pgstore/tpl_test.go
  - backend/internal/pgingest/tpl_test.go
touches_protected:
  - harness/config/test-lock.json
updates_docs:
  - docs/design-docs/harness-guide.md
amends_tests:
  - go:backend/internal/pgingest::TestExpAStatementThatFailsInAUnitLeavesTheUnitUndoneAndTheRunGoesOnFromIt
  - go:backend/internal/pgingest::TestExpAnObjectIsOnlyACandidateForRetirementWhenAFeedThatRetiresMadeIt
  - go:backend/internal/pgingest::TestExpAnObjectThatARecordOfAnotherSourceStandsUnderIsNotACandidateAndOneThatNoRecordStandsUnderIs
  - go:backend/internal/pgingest::TestExpTheCandidatesForRetirementAreTheObjectsTheOldPipelineRetiresInTheSameCaptures
  - go:backend/internal/pgingest::TestExpTheExpansionDoesNotBeginOnARunWhoseIdentificationIsNotDoneAndTheComputationOfEverythingGoesByUnitsOfRecords
  - go:backend/internal/pgingest::TestExpTheExpansionOfARunOfAWorldThatHasACaseForEveryRule
  - go:backend/internal/pgingest::TestExpTheSetIsTheSameForAnyOrderOfTheRowsReceivedAndOfTheNumbersOfTheRecords
  - go:backend/internal/pgingest::TestExpWhatManyRecordsOfARunShareIsReadOnceForAUnitSoTheRowsReadGoWithTheNumberOfRecords
  - go:backend/internal/pgingest::TestHkaASignalIsMarkedOnceOnEveryRowOfTheSourceAndNeverTwice
  - go:backend/internal/pgingest::TestIdnAChainOfRecordsInAnyOrderIsOneObjectWhenTheTurnsEndBeforeItsEndAndGoEndsTheSearchAndNothingIsLeftWhenItCannotHoldIt
  - go:backend/internal/pgingest::TestIdnAChainOfRecordsLongerThanTheTurnsOfTheDefaultIsOneObjectAndTheTurnsItTakesGrowMoreSlowlyThanItsLength
  - go:backend/internal/pgingest::TestIdnAChainThatIsLeftAndARecordOfALaterRoundThatSharesAKeyWithItAreLeftTogetherAndThenMadeOneObject
  - go:backend/internal/pgingest::TestIdnAGroupOfSeveralObjectsIsReadOnceForEachKeyAndWhatAUnitReadsOfItGrowsWithTheGroupAndNoFaster
  - go:backend/internal/pgingest::TestIdnARecordThatGivesNoKeyKeepsItsClaimsOnceAndIsNotReadAgainFromItsCellsWhenItCameInTheRowsReceived
  - go:backend/internal/pgingest::TestIdnARecordThatRepeatsItsKeyIsIdentifiedAsTheRecordItIsAndMakesOneObjectNotTwo
  - go:backend/internal/pgingest::TestIdnARecordTheComparisonClosedOrLetGoOfTakesNoPartAndMakesNoObjectKeyOrIdentifierWhileTheOtherRecordsAreIdentified
  - go:backend/internal/pgingest::TestIdnARecordWhoseReferenceIsNotSolvedIsReadAgainFromItsCellsAndBoundWithoutTouchingTheOnesThatAre
  - go:backend/internal/pgingest::TestIdnARoundWhoseSearchIsStoppedByItsLimitBindsNoGroupThatIsNotWholeAndLeavesTheRecordsOfItUnresolvedAndCounted
  - go:backend/internal/pgingest::TestIdnARunThatDoesNotOwnTheTablesOfTheComputationNeitherBeginsNorGoesOnWithTheIdentification
  - go:backend/internal/pgingest::TestIdnARunThatHasFewerRoundsThanTheKeysNeedLeavesTheRecordsUnresolvedAndSaysSo
  - go:backend/internal/pgingest::TestIdnARunThatStopsGoesOnFromTheNextUnitAndMakesWhatARunThatDoesNotStopMakes
  - go:backend/internal/pgingest::TestIdnARunThatStopsWhileGoEndsTheSearchForTheGroupsGoesOnAndMakesWhatARunThatDoesNotStopMakes
  - go:backend/internal/pgingest::TestIdnAnObjectWhoseKeyHasAParentIsMadeOnceTheParentIsAndItsIdComesFromGo
  - go:backend/internal/pgingest::TestIdnAnUnresolvedRecordOfASourceThatHasNoPlanInTheRunIsLeftAloneAndCountedAndDoesNotStopTheRunOfAnotherSource
  - go:backend/internal/pgingest::TestIdnAnUnresolvedRecordOfAnotherSourceIsIdentifiedByTheFeedsOfItsOwnSourceAndFindsTheObjectOfThisRun
  - go:backend/internal/pgingest::TestIdnAnUnresolvedRecordOfAnotherSourceWhoseFeedMakesNoObjectAndFindsNoneStaysUnresolvedWhileTheFeedOfThisSourceThatMakesObjectsDoesNotRead
  - go:backend/internal/pgingest::TestIdnEveryStatementOfAUnitReadsTheRegistryByItsIndexesAndWhatItReadsOfItDoesNotGrowWithTheSource
  - go:backend/internal/pgingest::TestIdnEveryStatementOfAUnitThatFailsLeavesTheRunWhereItWasAndTheRunGoesOnToTheSameEnd
  - go:backend/internal/pgingest::TestIdnEveryStatementOfTheClaimsAndOfTheLookupThatFailsIsReportedAndTheNextRunIsTheSame
  - go:backend/internal/pgingest::TestIdnMeasureTheIdentificationOfAGroupOfManyObjects
  - go:backend/internal/pgingest::TestIdnMeasureTheIdentificationOfARunWithAChangeOfOneInTen
  - go:backend/internal/pgingest::TestIdnTheCallsOfTheIdentificationThatAreMadeOutOfOrderOrOnARunThereIsNotAreRefused
  - go:backend/internal/pgingest::TestIdnTheEndsOfTheLinksAndTheClaimsOfARecordGoErasedFromItsCellsAreKeptWithTheKeys
  - go:backend/internal/pgingest::TestIdnTheFeedsOfTheIdentificationAreNamedBySourceAndNumberAndTheRunsOwnSourceMustHaveSome
  - go:backend/internal/pgingest::TestIdnTheIdentificationOfARunThatHasBegunOrWhoseComparisonIsNotDoneIsNotBegunAndWhatItMadeIsNotChanged
  - go:backend/internal/pgingest::TestIdnTheKeysOfARecordFindNoObjectOneObjectOrSeveralAndTheGroupsAreMadeWhateverTheOrder
  - go:backend/internal/pgingest::TestIdnTheLabelsOfTheTurnsBeforeTheLastTwoAreTakenAwayAndTheRowsOfThemAreAtMostTwoForEachPair
  - go:backend/internal/pgingest::TestIdnTheObjectsTheIdentificationMakesAreTheObjectsTheOldPipelineMakesForTheSameCaptures
  - go:backend/internal/pgingest::TestIdnTheRecordThatNamesAnObjectWhenTheRecordsGiveTheSameKeyIsTheOneWhoseDatasetAndKeyComeFirstWhateverTheOrderOfTheRows
  - go:backend/internal/pgingest::TestIdnTheRecordThatNamesTheObjectOfAGroupIsTheOneThatHasANameWhateverTheKeysOfTheOthersAndTheOrderOfTheRows
  - go:backend/internal/pgingest::TestIdnTheRecordsOfARunAreBoundToObjectsWhoseIdsAreTheOnesTheKeysGive
  - go:backend/internal/pgingest::TestIdnTheRecordsOfSmallUnitsWithReferencesNotSolvedAndWithEndsAreAllBound
  - go:backend/internal/pgingest::TestIdnTheSameChainWithTheLimitOfTheTurnsRaisedIsOneObjectAndMakesTheSameObjectInAnyOrderOfTheRows
  - go:backend/internal/pgingest::TestIdnTwoDatasetsThreeKindsOfKeyARepeatedKeyAndARecordThatIsClosedMakeTheObjectsTheOldPipelineMakes
  - go:backend/internal/pgingest::TestIdnTwoObjectsThatSayTheyHaveOneIdentifierGetItByTheRuleAndTheLoserIsToldAndAFeedThatMakesNoObjectFindsTheOnesThereAre
  - go:backend/internal/pgingest::TestIdnWhatTheTestsWatchOfTheStatementsOfAUnitIsOfTheStoreTheyAskItOfAndNotOfAnother
  - go:backend/internal/pgingest::TestIdnWhenEveryRecordIsInTheSamePlaceAUnitReadsOfTheRegistryWhatItDidWhenTheyWereInManyAndNoMore
  - go:backend/internal/pgingest::TestPgiACallThatNeedsATableThatIsDamagedFailsWithAnError
  - go:backend/internal/pgingest::TestPgiAKeyAnEndAndAClaimOfARowThatARuleFilteredOutAreHeldAndKeepTheNumberOfTheirFeed
  - go:backend/internal/pgingest::TestPgiAPageOfTenThousandRowsGoesInWithOneCopyOfEachTable
  - go:backend/internal/pgingest::TestPgiAPageThatFailsHalfWayLeavesNothingNotEvenItsPlaceAndNoRowIsHalfWritten
  - go:backend/internal/pgingest::TestPgiAPageWhoseRowOneTableRefusesLeavesNothingWhicheverTableItIsAndTheSinkGoesOn
  - go:backend/internal/pgingest::TestPgiARecordIsNewChangedOrTheSameAndOnlyNewAndChangedOnesCarryCellsKeysEndsAndClaims
  - go:backend/internal/pgingest::TestPgiARunMovesOnlyAlongTheStagesAndTheGateOfTheKGHoldsOneRunAtWork
  - go:backend/internal/pgingest::TestPgiASourceHasOneRunThatIsNotOver
  - go:backend/internal/pgingest::TestPgiASourcePutToAKGHasItsRowsAndItsPartitionsAndPuttingItAgainChangesNothingButTheDatasetsItAdds
  - go:backend/internal/pgingest::TestPgiAWriteOfAnAttemptThatIsNotTheRunsIsRefusedAndChangesNothing
  - go:backend/internal/pgingest::TestPgiAWriteOfAnOldAttemptAfterAnotherServerTookTheRunOverWritesNothing
  - go:backend/internal/pgingest::TestPgiDeletingAKGTakesItsSourcesOffFirstWithTheirRunsAndAKGWithNoSourcesIsDeletedAsBefore
  - go:backend/internal/pgingest::TestPgiEveryCallWithAContextThatHasEndedFailsAndChangesNothing
  - go:backend/internal/pgingest::TestPgiPuttingASourceToAKGAndTakingItOffHoldFewLocksAndDeletingAKGOfManySourcesFitsTheDefaultLockTable
  - go:backend/internal/pgingest::TestPgiReceiveStoresEveryPageWithItsPlaceAndMovesTheRunToComputing
  - go:backend/internal/pgingest::TestPgiReceivingAgainEmptiesTheSourcesPartitionAndBeginsAtTheFirstPageAndTheOtherSourcesAreLeftAlone
  - go:backend/internal/pgingest::TestPgiReceivingIsRefusedForAWrongPlanARunThatIsNotThereAndADatasetTheSourceWasNotPutWith
  - go:backend/internal/pgingest::TestPgiTakingASourceOffDropsItsPartitionsAndRowsInChunksAndRefusesASourceWithARunThatIsNotOver
  - go:backend/internal/pgingest::TestPgiThePlaceOfARecordIsItsRangeOnTheSheetElseItsLineAndTheLinesPassedOverAreCounted
  - go:backend/internal/pgingest::TestPgiTheProblemsOfARecordAreOnItsRowAndTheProblemsOfAPageOnTheRowMinusOneAndAnIncompleteDatasetIsToldToo
  - go:backend/internal/pgingest::TestPgiTheRowsOfAPageAreTheRowsThePreparerGaveWithTheCellsAndTheKeysIntact
  - go:backend/internal/pgingest::TestPgiTheTokenOfAPageIsWrittenAsTextAndReadBack
  - go:backend/internal/pgingest::TestRcpACellAFeedThatMakesNoObjectCannotReadIsAProblemOfTheRecordAndTheValueIsLeftOut
  - go:backend/internal/pgingest::TestRcpAPageIsDoneOnceAndAServerThatLostTheLeaseIsRefusedEvenToRead
  - go:backend/internal/pgingest::TestRcpARunStoppedBetweenPagesGoesOnFromTheNextPageAndStagesWhatOneThatDidNotStopStages
  - go:backend/internal/pgingest::TestRcpAStatementThatFailsLeavesTheRunWhereItWasAndWhatIsWrittenIsNotKept
  - go:backend/internal/pgingest::TestRcpAVolatileValueIsStagedAsAnEntryAndLeftOutOfTheDocument
  - go:backend/internal/pgingest::TestRcpAnObjectThatHasMoreLinesThanTheLimitIsLeftAsItIsAndCounted
  - go:backend/internal/pgingest::TestRcpAnObjectThatNoRecordGivesANameKeepsTheNameTheRegistryHasAndOneThatARecordNamesTakesTheValue
  - go:backend/internal/pgingest::TestRcpTheClaimRuleOfTheExpansionIsNeededForTheObjectsThatHoldAClaimToBeComputed
  - go:backend/internal/pgingest::TestRcpTheComputationIsRefusedBeforeTheExpansionIsDoneAndAPageThatIsNotTheNextOneIsRefused
  - go:backend/internal/pgingest::TestRcpTheObjectsTheRunTouchedAreTheSameWhenEveryRecordIsComputedAndTheOthersAreWhatTheRunBeforeMade
  - go:backend/internal/pgingest::TestRcpThePageSizeDoesNotChangeWhatIsStaged
  - go:backend/internal/pgingest::TestRcpTheReportIsWhatTheStatementsWroteAndAnObjectWithoutRecordsOrWithOnesThatCannotBeReadIsLeft
  - go:backend/internal/pgingest::TestRcpTheStagedObjectsAreTheOnesTheOldPipelineMakesFromTheSameCaptures
  - go:backend/internal/pgingest::TestRcpTwoRecordsOfOneObjectThatSayDifferentThingsHaveOneValueAndTheLosingOneIsAProblem
  - go:backend/internal/pgingest::TestRcpWhatManyRecordsOfAnObjectShareIsReadOnceSoTheRowsReadGoWithTheNumberOfLines
  - go:backend/internal/pgingest::TestRdfABucketReadsTheIndexRangeItCoversAndNotTheWholeTableAndWhatItReadsDoesNotGrowWithTheSource
  - go:backend/internal/pgingest::TestRdfABucketThatFailsLeavesNothingAndThePlaceStaysAtIt
  - go:backend/internal/pgingest::TestRdfACallWithACanceledContextIsRefusedByEveryStepOfTheComparison
  - go:backend/internal/pgingest::TestRdfADatasetReadAsWhatWasSeenCountsNoAbsenceAndADatasetLeftOutWholeCountsNothingAndADatasetThatNeverRetiresWaitsForNothing
  - go:backend/internal/pgingest::TestRdfADatasetTheReceivingDidNotGiveIsLeftOutWhole
  - go:backend/internal/pgingest::TestRdfAKeyHasOneReasonByOrderOfWeightSoWhatIsLetGoOfIsToldByTheWaitAndWhatIsShownAgainByTheCounts
  - go:backend/internal/pgingest::TestRdfAKeyThatRepeatsKeepsItsProblemFromItsStartThroughTheRunsThatDoNotChangeItToItsEnd
  - go:backend/internal/pgingest::TestRdfARowWhoseHashIsTheSameAndWhoseFilteringTurnsOverMakesTheObjectsTheOldPipelineMakes
  - go:backend/internal/pgingest::TestRdfARunThatFindsTheTablesOfTheComputationAnotherRunsHasNotBegunAndDoesTheComparisonAgainFromItsStart
  - go:backend/internal/pgingest::TestRdfAServerThatTakesTheRunOverGoesOnFromTheBucketTheRunIsAtAndCountsAtTheTimeTheComparisonBeganAt
  - go:backend/internal/pgingest::TestRdfAStepThatTheDatabaseRefusesIsToldAndTheBucketLeavesNothing
  - go:backend/internal/pgingest::TestRdfAWordOfCoverageThatIsNotOneOfThreeIsReadAsIncompleteAndALineWithNoWordIsReadByItsCompleteFlag
  - go:backend/internal/pgingest::TestRdfEveryFateOfAKeyIsToldAndTheWaitsAreCountedAtTheTimeOfTheRun
  - go:backend/internal/pgingest::TestRdfTheBucketsAreRangesOfDatasetAndKeyThatFollowOneAnotherAndTheLastHasNoEnd
  - go:backend/internal/pgingest::TestRdfTheComparisonDoesNotBeginForARunThatIsNotComputingOrForAnotherAttempt
  - go:backend/internal/pgingest::TestRdfTheComparisonGivesTheRawRecordsTheWaitsTheCountsAndTheLiveObjectsTheOldPipelineGivesForTheSameCaptures
  - go:backend/internal/pgingest::TestRdfTheComparisonGivesWhatTheOldPipelineGivesForRandomCapturesAndAnyCutOfTheBuckets
  - go:backend/internal/pgingest::TestRdfTheComparisonReadsTheCoverageOfTheReceivingFromTheCountsAndTakesTheOldFlagWhereThereIsNone
  - go:backend/internal/pgingest::TestRdfTheCountsOfTheRunSayForEachDatasetHowMuchOfItTheNextStepMayCount
  - go:backend/internal/pgingest::TestRdfWhatIsMadeDoesNotDependOnHowTheWorkIsCutIntoBuckets
  - go:backend/internal/pgingest::TestRdfWhatTheComparisonMadeOfARunIsWhatTheNextRunStartsFrom
  - go:backend/internal/pgstore::TestAChunkLargerThanOneStatementIsWrittenWhole
  - go:backend/internal/pgstore::TestFeedsStopAtReadableThroughAndWaitsEndWhenTheyShould
  - go:backend/internal/pgstore::TestIdentifiersOfListsTheLiveIdentifiersOfOneObject
  - go:backend/internal/pgstore::TestNumformTheSameNumberInAnotherFormIsTheSameValueInTheRowPathToo
  - go:backend/internal/pgstore::TestPostgresStorePassesThePreviewSuite
  - go:backend/internal/pgstore::TestPostgresStorePassesTheSuite
  - go:backend/internal/pgstore::TestRowsThatDoNotMatchTheHeadAreRefusedAndLeaveNothingBehind
  - go:backend/internal/pgstore::TestRsmALeaseIsGivenAfterTheAbandonThatWasCutIsEnded
  - go:backend/internal/pgstore::TestRsmALeaseIsGivenAfterTheRestoreThatWasCutIsEndedAndTheKGIsOpenAgain
  - go:backend/internal/pgstore::TestRsmALeaseOfAKGWithNoWorkHalfDoneIsGivenAtOnce
  - go:backend/internal/pgstore::TestRsmTheServerEndsTheWorkOfEveryKGAndOneThatCannotBeEndedDoesNotStopTheOthers
  - go:backend/internal/pgstore::TestSapAChunkThatWasNotCommittedIsApplyAgain
  - go:backend/internal/pgstore::TestSapAChunkWhoseCommitGotNoAnswerIsJudgedByTheHead
  - go:backend/internal/pgstore::TestSapAbandonBetweenChunksUndoesKeysAndTheApplyCanStartAgain
  - go:backend/internal/pgstore::TestSapAbandonCutInTheMiddleStopsTheStagedApplyUntilItIsEnded
  - go:backend/internal/pgstore::TestSapChunkDigestsOfSQLAreThoseOfGo
  - go:backend/internal/pgstore::TestSapDamagedDifferenceFailsTheChainOfAnOpenApply
  - go:backend/internal/pgstore::TestSapDamagedTablesFailTheStagedApplyWithAnErrorAndLeaveTheHeadAlone
  - go:backend/internal/pgstore::TestSapDifferenceThatDoesNotStartAtItsFirstChunkIsRefused
  - go:backend/internal/pgstore::TestSapEmptyDifferenceLeavesTheHeadAlone
  - go:backend/internal/pgstore::TestSapLocksOfAChunkDoNotGrowWithItsRows
  - go:backend/internal/pgstore::TestSapRetiredObjectTakesItsLinksWithIt
  - go:backend/internal/pgstore::TestSapStagedApplyAndRowsPathGiveTheSameStateAndReads
  - go:backend/internal/pgstore::TestSapStagedApplyGoesOnAfterACrashBetweenChunks
  - go:backend/internal/pgstore::TestSapStagedApplyRefusesWhatIsNotTheCallers
  - go:backend/internal/pgstore::TestSapStagedChunkOfAStalePositionIsRefused
  - go:backend/internal/pgstore::TestSapStagedVolatileValueThatCannotBeWrittenIsRefused
  - go:backend/internal/pgstore::TestSapVolatileValuesGoInIntervalsAndAreWrittenOnlyWhenTheyDiffer
  - go:backend/internal/pgstore::TestSvmAWriteIsRefusedWhenAnotherBuildAdjustedTheSchemaToAnotherGenerator
  - go:backend/internal/pgstore::TestTixAnObjectIsInTheTableOfItsOwnTypeOnlyAndAParentTypeHasAnEmptyTable
  - go:backend/internal/pgstore::TestTixTheNameIndexesAnswerTheExactNameAndTheStartOfTheNameWithoutCase
  - go:backend/internal/pgstore::TestV2StorePassesThePreviewSuite
  - go:backend/internal/pgstore::TestV2StorePassesTheSuite
  - go:backend/internal/pgstore::TestV2cGuardChecksInOrderAndTheHeadRoundTrips
  - go:backend/internal/pgstore::TestV2cLeaseIsTakenRenewedAndReleasedByTheDatabaseClock
  - go:backend/internal/pgstore::TestV2cLeaseRunsOutByTheClockOfTheCallerWhenTheStoreIsAskedTo
  - go:backend/internal/pgstore::TestV2cResolveDecidesAnUnknownCommitByTheHead
  - go:backend/internal/pgstore::TestV2dEveryOperationFailsOnAClosedPool
  - go:backend/internal/pgstore::TestV2dReadsFailOnRowsThatHoldWhatTheStoreDidNotWrite
  - go:backend/internal/pgstore::TestV2dReadsFailWhenATableTheyNeedIsMissing
  - go:backend/internal/pgstore::TestV2eALeaseAndAHeadThatAreDamagedFail
  - go:backend/internal/pgstore::TestV2eAServerThatHoldsAnotherModelRefusesTheKG
  - go:backend/internal/pgstore::TestV2eReadPointsAndVersions
  - go:backend/internal/pgstore::TestV2eTheCatalogRefusesDamagedRowsAndAnUnknownModel
  - go:backend/internal/pgstore::TestV2mAChunkWithAValueThatHasNoPlaceIsRefused
  - go:backend/internal/pgstore::TestV2mASealThatIsNotReadyOrDoesNotFollowIsRefused
  - go:backend/internal/pgstore::TestV2mAnUnknownOutcomeOfTheStartOfAWorkIsDecidedByTheHead
  - go:backend/internal/pgstore::TestV2mWritesAndFeedsFailWhenATableIsGone
  - go:backend/internal/pgstore::TestV2rCatalogListsGetsAndDeletesAKG
  - go:backend/internal/pgstore::TestV2rReadsLinksIdentifiersAndVolatileValues
  - go:backend/internal/pgstore::TestV2rReadsTheNewestAndThePastStates
  - go:backend/internal/pgstore::TestV2rRefusesReadsThatCannotBeAnswered
  - go:backend/internal/pgstore::TestV2wAChangeOfValuesAloneDoesNotTouchTheRowsOfTheObjectAndTheRowsAreWrittenInOrder
  - go:backend/internal/pgstore::TestV2wARefThatCameBackWithAnotherTypeIsRefused
  - go:backend/internal/pgstore::TestV2wARetiredObjectComesBackWithTheSameNumberAndTheKeyStaysBound
  - go:backend/internal/pgstore::TestV2wAWriteIsRefusedWhenTheSchemaWasMadeFromAnotherModel
  - go:backend/internal/pgstore::TestV2wAbandonTakesTheChunksOutOneNumberAtATimeAndLeavesNoTraceAndAnInterruptedOneIsEnded
  - go:backend/internal/pgstore::TestV2wBulkReadsGiveWhatReadsOneAtATimeGive
  - go:backend/internal/pgstore::TestV2wRestoreClosesTheKGUntilItEndsAndAnInterruptedOneIsResumed
  - go:backend/internal/pgstore::TestV2wTheSettingsOfTheTablesAreInForce
  - go:backend/internal/pgstore::TestVcolAnUnknownValueIsAnEntryWithAnEmptyColumn
  - go:backend/internal/pgstore::TestVcolEveryKindOfValueGoesThroughItsColumnAndComesBack
  - go:backend/internal/pgstore::TestVcolTheStagedApplyComparesTheColumnAndTheGradeAndTheBasis
  - go:backend/internal/pgstore::TestVcolTheWriterPutsTheValueInItsColumnAndTheReaderGivesItBack
  - go:backend/internal/pgstore::TestVersionsCarryTheirPartsAndRestoreDropsTheLaterOnes
  - go:backend/internal/pgstore::TestVgateARefusedValueIsAProblemOfTheBatchAndALongestNameIsKept
  - go:backend/internal/pgstore::TestVgateWhatTheGateLetsThroughComesBackFromTheColumnAsItWent
  - go:backend/internal/pgstore::TestWritesAndOldReadersAreHeldToTheirGenerationLeaseAndHead
test_change_reason: 요구사항이 아니라 테스트의 준비가 바뀐다. 준비 도우미(pgiOpen, rdfNew, v2rWorldWith, newRig, sapLoadBothInto, sapStagedInto)가 같은 상태를 한 번 만든 템플릿 데이터베이스를 복제해 받고 테스트를 나란히 돌린다. 적합성 스위트 넷과 TestV2mWritesAndFeedsFailWhenATableIsGone은 하위 테스트 첫 줄에 t.Parallel()만 더했다. 어느 테스트의 단언, 기대값, 사례, 테스트 수도 바뀌지 않았고, 복제본이 느린 길로 만든 데이터베이스와 같음을 Tpl 테스트가 비교한다. 지문은 도우미의 본문이 바뀌어 달라진다.
---
# ADR-0120 테스트 데이터베이스를 템플릿 데이터베이스의 복제로 준비하고 테스트를 나란히 돌린다

## 맥락

병합 관문 `gate`는 `go test -count=1 -coverprofile=… ./...`를 시간 제한 없이 돌려 패키지마다 기본 한도 600초를 받는다. 이 한도는 바꾸지 않는다. PostgreSQL에서 도는 패키지 둘이 한도에 붙었다(이 기계, CPU 2개, `-cover -v`): `adapter.pgingest` PGI_BEFORE초, `adapter.pgstore` PGS_BEFORE초. 수집의 새 흐름은 앞으로 테스트를 더 붙인다.

테스트 하나의 준비를 단계별로 재면(네 번, 밀리초) 이렇다.

| 단계 | 시간 |
|---|---|
| 빈 데이터베이스 만들기(`CREATE DATABASE`) | 23~67 |
| `pgstore.Open`(마이그레이션 7개와 공유 테이블) | 86~155 |
| `pgconn.Open`(마이그레이션이 끝난 데이터베이스) | 9~20 |
| `CreateKG`(표준 모델의 저장 스키마, 통계 객체 있음) | 868~1671 |
| `CreateKG`(통계 객체 없음) | 673~981 |
| `AttachSource`(소스 하나와 데이터셋 셋) | 19~33 |
| 끝난 데이터베이스를 템플릿으로 복제(`STRATEGY WAL_LOG`, 기본) | 141~281 |
| 같은 복제(`STRATEGY FILE_COPY`) | 98~668 |
| 복제본에 `pgstore.Open` | 9~16 |

데이터베이스 하나는 21MB다. 준비의 대부분은 KG의 저장 스키마를 만드는 문장들이고, 복제는 그 몇 분의 일이다.

## 결정

1. **템플릿 데이터베이스를 복제한다.** `pgconn.NewTestDatabaseFrom(tb, key, build)`가 키의 템플릿을 서버에서 찾고, 없으면 만든 뒤 `CREATE DATABASE … TEMPLATE …`로 복제해 준다. 복제본은 지금처럼 테스트가 끝나면 지운다. 복제 방식은 기본(`WAL_LOG`)이다. `FILE_COPY`는 체크포인트 때문에 들쭉날쭉했다.
2. **이름은 키의 해시다.** 템플릿의 이름은 `praxium_tpl_`과 키(와 만드는 방식의 판 `testTemplateForm`)의 SHA-256 앞 12바이트다. 키에는 템플릿의 내용을 정하는 것을 빠짐없이 넣는다.
   - 마이그레이션 템플릿(`pgconn.NewMigratedTestDatabase`): 저장 형식 번호와, 내장한 마이그레이션 파일마다 번호, 이름, 체크섬(`TestMigrationsKey`).
   - KG 템플릿(`pgstore.NewTestDatabaseWithKG`): 위의 것, 모델이 첫 KG에 만드는 저장 스키마 계획의 문장 전부와 매니페스트(생성기 버전, 모델 해시, 검색 집합, 통계 객체가 다 들어 있다), KG의 모든 필드(`%+v`), 그리고 호출한 테스트가 그 위에 하는 일을 적은 글(`thenKey`).
3. **만들기는 한 번이다.** 한 프로세스 안에서는 이름마다 뮤텍스가, 프로세스 사이에서는(`go test`는 패키지를 프로세스로 나란히 돌린다) 관리 데이터베이스의 advisory lock(키는 이름의 해시)이 막는다. 잠금을 잡은 뒤 이름을 다시 찾는다.
4. **다 만든 뒤에만 보인다.** 템플릿은 다른 이름(`<이름>_b<임의>`)으로 짓고, 빌드 함수가 연결을 모두 닫은 뒤 `IS_TEMPLATE true ALLOW_CONNECTIONS false`로 바꾸고, 마지막에 템플릿의 이름으로 바꾼다. 도중에 죽은 프로세스는 찾히지 않는 이름만 남긴다. 연결을 받지 않으니 복제가 "다른 사용자가 쓰는 중"으로 거절될 길이 없다(autovacuum 작업자는 서버가 끝낸다).
5. **실패는 실패다.** 템플릿을 만들지 못하거나 복제가 거절되면 테스트가 실패한다. 다시 시도하지 않고, 느린 길로 돌아가지 않는다. 실패한 빌드의 데이터베이스는 지운다.
6. **플래너 통계를 남기지 않는다.** 복제는 `pg_statistic`까지 복사한다. 빌드가 끝난 데이터베이스에 플래너 통계(`pg_stats`, `pg_stats_ext`)가 한 줄이라도 있으면 템플릿으로 쓰지 않고 실패한다. 그래서 복제본의 플래너는 새로 만든 데이터베이스와 같은 것을 본다. 누적 통계(`pg_stat_*`의 행 수)는 데이터베이스마다라서 복제본에서 0으로 시작한다. 새로 만든 데이터베이스는 준비 때 넣은 몇 행만큼의 수를 가지므로, 자동 분석이 도는 시점이 그만큼 다를 수 있다.
7. **템플릿은 서버에 남긴다.** 테스트 서버는 버리는 서버(`scripts/with-postgres.sh`, CI)라서 지우지 않는다. 키가 다르면(마이그레이션이나 모델이 바뀌면) 새 이름이라 낡은 것을 받지 않는다. 버리지 않는 자기 서버를 쓰면 코드를 바꾼 뒤 `praxium_tpl_`로 시작하는 데이터베이스를 지운다. 키에 넣을 수 없는 것(마이그레이션 실행기, `CreateKG`, `AttachSource`가 Go에서 쓰는 행)이 바뀐 채 낡은 템플릿을 받으면 아래 비교 테스트가 실패한다.
8. **쓰는 곳.**
   - `pgstore.NewTestStore`, `NewTestStoreWith`: 마이그레이션 템플릿. 적합성 스위트와 `adapter.pgcheck`, `adapter.identitycheck`의 테스트가 이것을 쓴다.
   - `adapter.pgstore`의 `v2rWorldWith`(저장 스키마를 직접 읽고 쓰는 테스트들의 세계): KG 템플릿(통계 객체 없는 표준 모델).
   - `adapter.pgingest`의 `pgiOpen`: 마이그레이션 템플릿. `rdfNew`(수집의 새 흐름 테스트 대부분과 `exp`, `idn`, `rcp`의 세계): 첫 KG와 플랜의 소스를 붙인 KG 템플릿. 소스 번호는 다시 붙이지 않고 읽는다(`rdfRefOf`). `AttachSource`를 다시 부르면 시퀀스 번호를 먹어 그 뒤의 번호가 달라진다.
   - 빈 데이터베이스가 필요한 테스트(`pgconn.NewTestDatabase`, `NewTestPool`, `pgstore.NewTestDatabase`, `sxOpen`, 마이그레이션과 옛 테이블의 관문 테스트)는 그대로다.
9. **복제본은 새로 만든 것과 같다.** `pgconn.DumpTestDatabase`는 시스템 밖의 모든 릴레이션(종류, `relpages`, `reltuples`), 컬럼, 인덱스, 제약, 트리거, 뷰, 함수, 통계 객체, 시퀀스 값, 모든 테이블의 모든 행, 플래너 통계의 행 수를 줄로 낸다(서버 시계로 하루 안의 시각은 `recent`로). 세 테스트가 복제본과 느린 길로 만든 데이터베이스를 이것으로 비교한다.
10. **준비가 싸진 테스트는 나란히 돈다.** 준비만 줄이면 `adapter.pgingest`는 76%에 머물렀다(남은 시간의 대부분이 데이터를 다루는 일이다). 테스트마다 자기 데이터베이스가 있으므로 도우미가 최상위 테스트를 `t.Parallel()`로 돌린다. `pgconn.TestInParallel`이 한 테스트에 한 번만 부르고(두 번 부르면 Go가 멈춘다), 하위 테스트는 건드리지 않는다(부모가 하위 테스트 뒤에 그 결과를 읽을 수 있다). 부르는 곳은 `pgiOpen`, `rdfNew`, `v2rWorldWith`, `newRig`, `sapLoadBothInto`, `sapStagedInto`이고, 적합성 스위트 넷과 `TestV2mWritesAndFeedsFailWhenATableIsGone`은 하위 테스트를 나란히 돌린다(사례마다 저장소가 따로다). 나란히 돌지 않는 것은 목록(`pgiSerial`, `v2rSerial`, `sapSerial`)에 이름으로 둔다: 플래너가 읽는 행 수나 계획을 단언하는 테스트(계획이 ANALYZE와 autovacuum의 통계에서 오므로 함께 도는 일이 그 시점을 바꿀 수 있다), 서버가 모든 연결에 함께 쓰는 잠금 테이블을 재는 테스트, 잠금 수를 세는 테스트. Go는 이것들을 나란히 도는 테스트보다 먼저 하나씩 돌린다. 환경 변수를 바꾸는 테스트(`t.Setenv`)는 이 도우미를 쓰지 않는다. 동시에 도는 수는 `go test`의 기본(`GOMAXPROCS`)이고 관문의 명령은 그대로다. 도우미를 쓰는 새 테스트는 기본으로 나란히 돈다. 공용 상태(환경 변수, 패키지 전역, 서버 전체의 설정이나 잠금 테이블, 플래너의 계획)를 쓰는 테스트를 더하면 목록에 이름을 올린다.

## 예외 상황과 막는 테스트

| 상황 | 막는 것 | 테스트 |
|---|---|---|
| 템플릿이 낡음 | 키가 마이그레이션, 저장 형식, 계획 문장, 매니페스트, KG 필드, `thenKey`를 담는다. 키에 없는 Go 코드의 변경은 비교 테스트가 잡는다 | `TestTplTheKeyOfAKGTemplateChangesWithEverythingThatDecidesWhatTheTemplateHolds`, `TestTplACopyOfTheMigratedTemplateIsTheDatabaseThatOpenMakesFromAnEmptyOne`(키에 모든 마이그레이션) |
| 여러 테스트·프로세스가 한꺼번에 | 뮤텍스, advisory lock, 잠근 뒤 다시 찾기 | `TestTplTestsOfOneProcessThatAskForATemplateAtOnceMakeItOnce`, `TestTplAProcessWaitsForTheOneThatMakesTheTemplateAndThenCopiesWhatItMade` |
| 만들다 실패, 플래너 통계가 남음 | 실패하고 아무것도 남기지 않으며, 다음 요청이 새로 만든다 | `TestTplATemplateThatCannotBeMadeFailsTheTestAndLeavesNothingBehind`, `TestTplAKGTemplateThatCannotBeMadeFailsTheTestAndIsNotMadeAnotherWay` |
| 통계가 복사됨 | 템플릿에 플래너 통계를 두지 않는다. 비교의 줄에 `relpages`, `reltuples`, 플래너 통계의 행 수가 있다. 규모 테스트는 혼자 돈다 | 위의 테스트와 비교 테스트, 규모 테스트 여섯을 세 번씩 돌린 결과(아래) |
| 나란히 돌 때 공용 상태 | 한 번만, 최상위만, 목록의 테스트는 혼자 | `TestTplATestIsMadeParallelOnceWhateverHelpersAskAndASubtestIsLeftAsItIs` |
| 복제가 거절됨 | 다른 길 없이 실패 | `TestTplACopyThatTheServerRefusesFailsTheTestAndIsNotMadeAnotherWay` |
| 서버가 없음 | `NewTestDatabase`와 같이 실패, DSN을 보이지 않음 | `TestTplATemplateNeedsTheTestServerAsATestDatabaseDoes` |
| 복제본이 새로 만든 것과 다름 | 비교 | `TestTplATemplateIsMadeOnceAndEveryCopyIsADatabaseOfItsOwnWithWhatTheTemplateHas`, `TestTplACopyOfTheMigratedTemplateIsTheDatabaseThatOpenMakesFromAnEmptyOne`, `TestTplACopyOfAKGTemplateIsTheDatabaseThatOpenAndCreateKGMakeTheSlowWay`, `TestTplTheDatabaseATestOfTheReceivingStartsFromIsTheOneOpenCreateKGAndAttachSourceMakeTheSlowWay` |

## 질의 스키마(ADR-0113, 아직 main에 없음)와 이을 때

ADR-0113은 마이그레이션 `0008_install_token`이 데이터베이스마다 무작위 설치 토큰을 만들고, 질의 스키마의 역할 이름에 그 토큰을 넣는다. 역할은 클러스터의 것이라 복제되지 않고, 같은 토큰의 데이터베이스 둘이 역할을 나누면 KG를 지울 수 없다. 그래서 이을 때 다음을 지킨다.

- 마이그레이션 템플릿의 복제 뒤에 토큰을 새로 낸다(`makeTestDatabase`가 템플릿에서 복제한 뒤 `install_token`이 있으면 `UPDATE install_token SET token = <새 12자리>`). 비교 테스트는 토큰 행만 빼고 비교하고, 두 복제본의 토큰이 다름을 단언한다.
- KG 템플릿은 질의 스키마가 없는 모델에만 쓴다(`NewTestDatabaseWithKG`가 질의 스키마를 만드는 모델을 거절한다). `pgiOpen`·`rdfNew`는 ADR-0113이 고른 가벼운 모델(`TestOnlyModelsWithoutQuerySchema`)로 바꾼다.
- 질의 스키마의 테스트(`Qsc*`)는 `sxOpen`과 `NewTestDatabase`로 빈 데이터베이스에서 시작하므로 복제본을 쓰지 않는다.

## 이유

- 시간이 드는 곳은 KG의 저장 스키마(문장 수백 개)를 만드는 일이고, 테스트 대부분이 같은 KG에서 시작한다. 한 번 만들고 파일 수준으로 복제하는 것이 가장 크게 줄인다.
- 키를 계획의 문장과 매니페스트의 해시로 잡으면 생성기나 모델이 바뀌는 모든 경우를 사람이 판 번호를 올리지 않아도 따라간다.
- 이름 바꾸기로 공개하면 반쯤 만든 템플릿이 보일 길이 없다.

## 대안과 기각 사유

- **시간 한도를 늘리거나 패키지를 쪼갠다**: 한도를 늘리는 것은 규칙을 느슨하게 하는 것이다. 쪼개면 같은 비용이 둘로 나뉠 뿐이고 테스트가 늘면 다시 붙는다.
- **템플릿 이름에 프로세스마다의 토큰을 넣는다**: 코드가 바뀌면 낡은 템플릿을 받을 길이 아예 없지만, 패키지마다 같은 템플릿을 다시 만들고 버리지 않는 서버에 쓰레기가 쌓인다. 비교 테스트가 낡은 것을 잡으므로 공유를 택했다.
- **`TestMain`에서 만들고 지운다**: 패키지마다 `TestMain`을 두어야 하고, 다른 프로세스와의 공유가 안 된다.
- **`CreateKG`를 한 번의 왕복으로 줄인다**: 제품 코드의 변경이라 이 결정의 범위 밖이다.

## 잠긴 테스트의 변경

`amends_tests`의 173개(`adapter.pgingest` 105, `adapter.pgstore` 68)는 단언이 하나도 바뀌지 않았다. 지문이 바뀐 까닭은 셋이다.

| 바뀐 것 | 걸린 테스트 |
|---|---|
| `pgiOpen`(마이그레이션 템플릿, 나란히), `pgiEnv.kg`(KG 필드를 `pgiKG`로 뺐다), `rdfNew`(KG 템플릿, 소스 번호를 읽기만 하는 `rdfRefOf`) | `adapter.pgingest`의 이 도우미를 쓰는 모든 테스트 |
| `v2rWorldWith`(KG 템플릿, 나란히), `newRig`, `sapLoadBothInto`, `sapStagedInto`(나란히) | `adapter.pgstore`의 이 도우미를 쓰는 테스트 |
| 하위 테스트의 첫 줄에 `t.Parallel()` | 적합성 스위트 넷, `TestV2mWritesAndFeedsFailWhenATableIsGone` |

요구사항은 더하지 않는다. 테스트의 준비와 실행 방식은 REQ-270(테스트는 실제 PostgreSQL에서 돌고 서버가 없으면 실패한다)과 REQ-023(테스트 잠금) 안의 구현이고, 제품의 동작은 바뀌지 않는다.

## 결과

RESULT_TEXT
