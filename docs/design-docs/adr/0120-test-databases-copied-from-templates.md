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
  - go:backend/internal/pgingest::TestExpAnObjectIsRetiredAsTheOldPipelineRetiresItWhenTheRecordOfAFeedThatAddsValuesChangesInEveryRun
  - go:backend/internal/pgingest::TestExpAnObjectThatARecordOfAFeedThatMakesObjectsStandsUnderBesideAChangedRecordOfAFeedThatAddsValuesIsNoCandidate
  - go:backend/internal/pgingest::TestExpAnObjectThatOnlyAChangedRecordOfAFeedThatAddsValuesIsBoundToIsACandidateWhoeverGoesOnWithTheExpansion
  - go:backend/internal/pgingest::TestExpWhatTheStatementsReadInThePlanTheServerChoosesGoesWithTheRecordsAndNotWithTheRestOfTheKG
  - go:backend/internal/pgingest::TestIdnTheQuestionsOfARoundAreReadByTheirIndexWithTheStatisticsTheRunGivesThePlanner
  - go:backend/internal/pgingest::TestExpAKeyThatTwoUnitsHaveIsReadByTheFirstOfThemAndByNoOtherAndTheKeysAreOfTheRun
  - go:backend/internal/pgingest::TestExpAReferenceOfAFeedThatOnlyAddsValuesIsNoSupportAndOneOfAFeedThatIsNotKnownIs
  - go:backend/internal/pgingest::TestExpAnObjectThatOnlyARecordOfAFeedThatAddsValuesStandsUnderIsRetiredAsTheOldPipelineRetiresIt
  - go:backend/internal/pgingest::TestExpAnObjectTwoFeedsMakeOfWhichOneRetiresIsRetiredAsTheOldPipelineRetiresItWhicheverFeedLetsGoOfItFirst
  - go:backend/internal/pgingest::TestExpTheExpansionOfEverythingTakesTheRecordsTheIdentificationReadsAndNoOther
  - go:backend/internal/pgingest::TestExpTheMarkOfTheRegistryIsOnTheObjectsThatARetirableReferenceOfTheRunPointsAtAndStays
  - go:backend/internal/pgingest::TestExpTheNumbersOfTheRecordsOfARunAreAsManyAsTheRecordsWhateverTheRulesLookedAt
  - go:backend/internal/pgingest::TestExpTheRecordOfALinkThatEndsAtTheObjectAnEndKeyOfARecordFindsIsReadAgain
  - go:backend/internal/pgingest::TestExpTheRecordOfALinkThatStartsAtTheObjectAChangedRecordIsNowBoundToIsReadAgain
  - go:backend/internal/pgingest::TestExpTheRecordOfASecondLinkThatEndsWhereTheLinkOfAChangedRecordEndedIsReadAgain
  - go:backend/internal/pgingest::TestExpTheReferencesOfRecordsOfTwoSourcesWhoseFeedsHaveTheSameNumberAreRetirableByTheFeedOfTheirOwnSource
  - go:backend/internal/pgingest::TestExpTheUnitsDoneAreTheUnitsTheExpansionWentByAndTheRunBeginsAtTheFirstRule
  - go:backend/internal/pgingest::TestExpWhileTheIdentificationLeavesPairsNoObjectIsACandidateForRetirementWhoeverGoesOnWithTheExpansion
  - go:backend/internal/pgingest::TestRcpAServerThatTakesTheRunOverUsesWhatTheRunBeganWithAndRefusesWhatIsDifferent
  - go:backend/internal/pgingest::TestRcpAnObjectOfTheRunThatNoRegistryAndNoNewObjectHasIsLeftAndDoesNotStopTheStep
  - go:backend/internal/pgingest::TestRcpAnObjectTheKGHasThatIsLeftIsToldAndIsNeverACandidateForRetirement
  - go:backend/internal/pgingest::TestRcpAnObjectTheRunWouldMakeThatHasTooManyLinesIsTakenBackWithEverythingThatNamedItAndTheRecordsUnderItAreProblems
  - go:backend/internal/pgingest::TestRcpAnObjectWhoseContextParentIsTakenBackIsTakenBackToo
  - go:backend/internal/pgingest::TestRcpTheComputationRefusesToBeginTwiceAndToRunOnTablesThatAreNotTheRunsAndAPageAfterTheEnd
  - go:backend/internal/pgingest::TestRcpTheObjectsThatAreTakenBackAreTheSameWhateverThePageSizeIs
  - go:backend/internal/pgingest::TestRcpTheSourceConflictsAreTheOnesTheOldPipelineReportsFromTheSameCaptures
  - go:backend/internal/pgingest::TestRcpWhatTheRunReadsIsTheReferencesThatStandAndOnlyOfTheObjectsThatAreNotCandidatesForRetirement
  - go:backend/internal/pgstore::TestAChangeThatIsNotAnAdditionIsRefusedAndTheDatabaseIsAsItWas
  - go:backend/internal/pgstore::TestAMoreCompleteModelIsFollowedByAdditionsAndTheTablesThatHadRowsAreNotRewritten
  - go:backend/internal/pgstore::TestARunThatStoppedWhileBuildingLeavesInvalidAndStrayIndexesThatTheNextRunCleansUp
  - go:backend/internal/pgstore::TestAServerWhoseModelIsNotTheOneTheSchemaWasMadeFromIsTold
  - go:backend/internal/pgstore::TestAStorageSchemaThatFailsHalfWayLeavesNoSchemaAndNoRecord
  - go:backend/internal/pgstore::TestAnAdjustmentThatCannotMakeATableOrBuildAnIndexLeavesTheSchemaUsable
  - go:backend/internal/pgstore::TestAnIndexThatCannotBeBuiltLeavesTheStateIndexPendingAndTheNextRunFinishesIt
  - go:backend/internal/pgstore::TestCreatingAStorageSchemaTwiceOrAtTheSameTimeLeavesOneSchema
  - go:backend/internal/pgstore::TestCreatingRefusesWhatItCannotMakeASchemaFrom
  - go:backend/internal/pgstore::TestDroppingAStorageSchemaRemovesItAndItsNumberIsNotGivenAgain
  - go:backend/internal/pgstore::TestHeldAKGOfGeneratorThreeGetsTheColumnHeldOnItsTablesAndItsSourcePartitionsWhenItIsAdjusted
  - go:backend/internal/pgstore::TestIdnAKGOfGeneratorSixGetsTheHeldColumnsTheFeedOfABindTheWorkingTablesAndTheIndexesOfTheIdentificationWhenItIsAdjustedAndKeepsWhatItHas
  - go:backend/internal/pgstore::TestIngestAKGMadeByTheGeneratorBeforeIsAdjustedByAddingTheTablesWithoutClosingIt
  - go:backend/internal/pgstore::TestIngestARunKeepsToTheClosedStagesAndSaysHowItFailedInThreeWays
  - go:backend/internal/pgstore::TestIngestASourceHasPartitionsThatAreAttachedEmptiedAndDroppedOneByOne
  - go:backend/internal/pgstore::TestIngestMakingAndDroppingAKGOfTheStandardModelFitsInTheDefaultLockTable
  - go:backend/internal/pgstore::TestIngestPuttingASourceToAKGDoesNotWaitForAnotherSourceThatIsBeingReceived
  - go:backend/internal/pgstore::TestIngestSharedTablesHaveTheColumnsTheNamesSay
  - go:backend/internal/pgstore::TestIngestTablesOfANewKGAreLoggedAndHaveTheColumnsTheNamesSay
  - go:backend/internal/pgstore::TestIngestTheDigestOfAChunkInSQLIsTheDigestTheGoCodeGives
  - go:backend/internal/pgstore::TestIngestTheGateOfTheRunsAllowsOneOpenRunPerSourceAndOneWorkingRunPerKG
  - go:backend/internal/pgstore::TestPgsrcDeletingAKGOrDroppingItsSchemaTakesTheSourcesOffFirstEvenWithARunThatIsNotOver
  - go:backend/internal/pgstore::TestQscACustomAttributeIsAColumnNamedLikeItIsAndTheModelViewsDescribeOnlyWhatTheGradeSees
  - go:backend/internal/pgstore::TestQscADescriptionWithQuotesBackslashesAndStatementsComesBackAsTheTextItWasAndRunsNothing
  - go:backend/internal/pgstore::TestQscAFunctionOfAPastTimeAnswersForTheNumberAndRaisesAnErrorForOneThatCannotBeRead
  - go:backend/internal/pgstore::TestQscAFunctionOfTheCallerThatTheCostPutsFirstNeverSeesTheValueOfAnAttributeOfAHigherGradeOnAnySurface
  - go:backend/internal/pgstore::TestQscAGradeRoleHasNoRightOnAnotherKGOnTheStorageSchemaOrOnAHigherGrade
  - go:backend/internal/pgstore::TestQscAGradeRoleHasNoRightOnTheSetForALargeApplyOfAHigherGrade
  - go:backend/internal/pgstore::TestQscAGradeRoleSeesNoColumnOfAnAttributeOfAHigherGrade
  - go:backend/internal/pgstore::TestQscAHigherGradeSchemaHoldsOnlyWhatDiffersFromTheGradeBelow
  - go:backend/internal/pgstore::TestQscAKGMadeByTheCatalogHasItsQuerySchemaAndRolesAndAServerThatStartsMakesThemForAKGOfAnOlderGenerator
  - go:backend/internal/pgstore::TestQscARenameOfAnAttributeBuildsEveryViewAgainWithTheNewName
  - go:backend/internal/pgstore::TestQscARenameWithAnAddedAttributeBuildsTheFunctionOfAPastTimeAgainWithTheNewName
  - go:backend/internal/pgstore::TestQscAStatementOfTheQuerySchemaThatFailsLeavesTheKGAsItWasForTheMakingAndForTheAdjustment
  - go:backend/internal/pgstore::TestQscATokenThatIsNotOneOfTheInstallationRefusesEveryWayToMakeAdjustOrDropTheQuerySchema
  - go:backend/internal/pgstore::TestQscATypeViewHasTheObjectsOfTheTypeAndOfEveryTypeBelowItOnce
  - go:backend/internal/pgstore::TestQscATypeViewReadsOneObjectByAnIndexAndTheViewOfAParentIsAUnionAllOfTheTablesOfItsTypes
  - go:backend/internal/pgstore::TestQscAnErrorOfAConditionOfTheCallerNeverShowsOrTellsTheValueOrTheNameOfAnAttributeOfAHigherGrade
  - go:backend/internal/pgstore::TestQscDeletingAKGDropsItsQuerySchemasAndItsRoles
  - go:backend/internal/pgstore::TestQscEveryFunctionOfTheQuerySchemaHasAFixedSearchPathAndNoRightForThePublicAndNoRoleCanLogInOrMakeRoles
  - go:backend/internal/pgstore::TestQscEverySurfaceOfAGradeIsCutByTheSameSetOfAttributeNumbers
  - go:backend/internal/pgstore::TestQscEveryViewAndColumnOfTheQuerySchemaHasAComment
  - go:backend/internal/pgstore::TestQscEveryWriteOfAGradeRoleIsRefused
  - go:backend/internal/pgstore::TestQscTheChangeRecordsAreViewsOfTheKGAndTheVersionsOfIt
  - go:backend/internal/pgstore::TestQscTheDetailOfAnObjectHasEveryAttributeTheGradeSeesWithItsEvidenceAndIsNullForAnObjectThatIsNotThere
  - go:backend/internal/pgstore::TestQscTheFunctionsAndTheChangeViewsOfAGradeAreCutByTheSameSetOfAttributeNumbers
  - go:backend/internal/pgstore::TestQscTheLinksOfAnObjectAreReadFromTheIndexAloneByTheirNumber
  - go:backend/internal/pgstore::TestQscTheQuerySchemaOfTheStandardModelHasAViewForEveryTypeAndEveryLinkTypeAndTheTimeToBuildItIsKnown
  - go:backend/internal/pgstore::TestQscTheSetForALargeApplyHasOnlyWhatIsReadableThroughTheWatermark
  - go:backend/internal/pgstore::TestQscTheSetForALargeApplyHasTheObjectAtTheWatermarkAndNotTheObjectAfterItInTheViewsOfEveryObject
  - go:backend/internal/pgstore::TestQscTheStandardModelHasAFunctionOfAPastTimeForEveryTypeTheViewsOfTheChangesAndTheSixSchemas
  - go:backend/internal/pgstore::TestQscTheStepsOfTheQuerySchemaFailInATransactionThatIsOverAndAPlanWithoutThemDoesNothing
  - go:backend/internal/pgstore::TestQscTheTimeToBuildAllTheViewsAgainIsMeasuredByKindOfStatement
  - go:backend/internal/pgstore::TestRdfAKGOfGeneratorFourGetsTheColumnsAndTheOwnerTableOfTheComparisonWhenItIsAdjustedAndKeepsWhatItHas
  - go:backend/internal/pgstore::TestSapRowHashExpressionsGiveTheValuesOfTheGoFunctions
  - go:backend/internal/pgstore::TestSchemaStartupAdjustsAKGOfTheGeneratorBeforeAndOpensIt
  - go:backend/internal/pgstore::TestSchemaStartupLeavesAKGThatDiffersInMoreThanTheGeneratorRefused
  - go:backend/internal/pgstore::TestSchemaStartupReportsAFailureAndLeavesANewerKGAlone
  - go:backend/internal/pgstore::TestSchemaStartupSkipsAKGThatAnotherRunIsBuilding
  - go:backend/internal/pgstore::TestStatisticsThatWereLeftForLaterAreMadeOnceAndAnAdjustmentWaitsForThem
  - go:backend/internal/pgstore::TestSvmAKGThatKeptItsValuesInProvHasThemMovedIntoTheColumnsAndTheNameIndexesBuilt
  - go:backend/internal/pgstore::TestSvmAnIndexThatAnAdjustmentLeftInvalidIsBuiltAgainByTheNextRun
  - go:backend/internal/pgstore::TestSvmTheAdjustmentKeepsWhatHasNoColumnAndTakesOldTimesAndEmptiesAsTheyAre
  - go:backend/internal/pgstore::TestSvmTheStartOfAServerFinishesTheIndexesOfAKGThatIsOfThisGeneratorButNotSettled
  - go:backend/internal/pgstore::TestTheIndexOfTheSearchSetIsTheOneTheQueryPlannerChooses
  - go:backend/internal/pgstore::TestTheSchemaExecutorFailsWithAnErrorOnAClosedPoolAndOnDamagedRecords
  - go:backend/internal/pgstore::TestTheStorageSchemaOfAKGHasTheTablesOfThePlanAndNoIndexThatWasNotMarked
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
test_change_reason: 요구사항이 아니라 테스트의 준비가 바뀐다. 준비 도우미(pgiOpen, rdfNew, v2rWorldWith)가 같은 상태를 한 번 만든 템플릿 데이터베이스를 복제해 받고, 이것과 newRig, sapLoadBothInto, sapStagedInto, sxOpen, igtOpenStandard가 테스트를 나란히 돌린다(계획·시간·잠금을 재는 테스트는 이름 목록으로 빼서 혼자 돈다). 적합성 스위트 넷과 TestV2mWritesAndFeedsFailWhenATableIsGone은 하위 테스트 첫 줄에 t.Parallel()만 더했다. 어느 테스트의 단언, 기대값, 사례, 테스트 수도 바뀌지 않았고, 복제본이 느린 길로 만든 데이터베이스와 같음을 Tpl 테스트가 비교한다. 지문은 도우미의 본문이 바뀌어 달라진다. main을 합칠 때 먼저 병합된 ADR-0121의 새 테스트 다섯도 같은 도우미를 거쳐 지문만 바뀌었고, 그중 계획과 읽은 행을 재는 둘(TestIdnTheQuestionsOfARound…, TestExpWhatTheStatementsReadInThePlan…)은 혼자 도는 목록(pgiSerial)에 더했다.
decision_hash: 0d087e321c3ecdf9
---
# ADR-0120 테스트 데이터베이스를 템플릿 데이터베이스의 복제로 준비하고 테스트를 나란히 돌린다

## 맥락

병합 관문 `gate`는 `go test -count=1 -coverprofile=… ./...`를 시간 제한 없이 돌려 패키지마다 기본 한도 600초를 받는다. 이 한도는 바꾸지 않는다. PostgreSQL에서 도는 패키지 둘이 한도에 붙었다(CPU 2개인 개발 기계, `-cover -v`). ADR-0113을 들이기 전의 main에서 `adapter.pgingest`는 538~552초, `adapter.pgstore`는 349~374초였고, 들인 뒤의 main에서는 `adapter.pgingest`가 600초에서 끊겼고 `adapter.pgstore`는 488초였다. 수집의 새 흐름은 앞으로 테스트를 더 붙인다.

테스트 하나의 준비를 단계별로 재면(ADR-0113 전, 네 번, 밀리초) 이렇다.

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
   - KG 템플릿(`pgstore.NewTestDatabaseWithKG`): 위의 것, 모델의 갈래(통계 객체가 있는지, 질의 스키마가 있는지), 모델이 첫 KG에 만드는 저장 스키마 계획의 문장 전부와 매니페스트(생성기 버전, 모델 해시, 검색 집합, 통계 객체가 다 들어 있다), KG의 모든 필드(`%+v`), 그리고 호출한 테스트가 그 위에 하는 일을 적은 글(`thenKey`). 모델은 둘 중 하나다: 통계 객체가 있으면 서버의 모델에서 질의 스키마만 뺀 것(`TestOnlyModelsWithoutQuerySchema`), 없으면 `NewTestStore`의 가벼운 모델(`StandardModels(false)`). 질의 스키마를 만드는 모델에는 템플릿이 없다(아래 질의 스키마 절).
3. **만들기는 한 번이다.** 한 프로세스 안에서는 이름마다 뮤텍스가, 프로세스 사이에서는(`go test`는 패키지를 프로세스로 나란히 돌린다) 관리 데이터베이스의 advisory lock(키는 이름의 해시)이 막는다. 잠금을 잡은 뒤 이름을 다시 찾는다.
4. **다 만든 뒤에만 보인다.** 템플릿은 다른 이름(`<이름>_b<임의>`)으로 짓고, 빌드 함수가 연결을 모두 닫은 뒤 `IS_TEMPLATE true ALLOW_CONNECTIONS false`로 바꾸고, 마지막에 템플릿의 이름으로 바꾼다. 도중에 죽은 프로세스는 찾히지 않는 이름만 남긴다. 연결을 받지 않으니 복제가 "다른 사용자가 쓰는 중"으로 거절될 길이 없다(autovacuum 작업자는 서버가 끝낸다).
5. **실패는 실패다.** 템플릿을 만들지 못하거나 복제가 거절되면 테스트가 실패한다. 다시 시도하지 않고, 느린 길로 돌아가지 않는다. 실패한 빌드의 데이터베이스는 지운다.
6. **플래너 통계를 남기지 않는다.** 복제는 `pg_statistic`까지 복사한다. 빌드가 끝난 데이터베이스에 플래너 통계(`pg_stats`, `pg_stats_ext`)가 한 줄이라도 있으면 템플릿으로 쓰지 않고 실패한다. 그래서 복제본의 플래너는 새로 만든 데이터베이스와 같은 것을 본다. 누적 통계(`pg_stat_*`의 행 수)는 데이터베이스마다라서 복제본에서 0으로 시작한다. 새로 만든 데이터베이스는 준비 때 넣은 몇 행만큼의 수를 가지므로, 자동 분석이 도는 시점이 그만큼 다를 수 있다.
7. **템플릿은 서버에 남긴다.** 테스트 서버는 버리는 서버(`scripts/with-postgres.sh`, CI)라서 지우지 않는다. 키가 다르면(마이그레이션이나 모델이 바뀌면) 새 이름이라 낡은 것을 받지 않는다. 버리지 않는 자기 서버를 쓰면 코드를 바꾼 뒤 `praxium_tpl_`로 시작하는 데이터베이스를 지운다. 키에 넣을 수 없는 것(마이그레이션 실행기, `CreateKG`, `AttachSource`가 Go에서 쓰는 행)이 바뀐 채 낡은 템플릿을 받으면 아래 비교 테스트가 실패한다.
8. **쓰는 곳.**
   - `pgstore.NewTestStore`, `NewTestStoreWith`: 마이그레이션 템플릿. 적합성 스위트와 `adapter.pgcheck`, `adapter.identitycheck`의 테스트가 이것을 쓴다.
   - `adapter.pgstore`의 `v2rWorldWith`(저장 스키마를 직접 읽고 쓰는 테스트들의 세계): KG 템플릿(통계 객체 없는 표준 모델).
   - `adapter.pgingest`의 `pgiOpen`: 마이그레이션 템플릿(저장소는 ADR-0113대로 `TestOnlyModelsWithoutQuerySchema`로 연다). `rdfNew`(수집의 새 흐름 테스트 대부분과 `exp`, `idn`, `rcp`의 세계): 같은 모델로 첫 KG를 만들고 플랜의 소스를 붙인 KG 템플릿. 소스 번호는 다시 붙이지 않고 읽는다(`rdfRefOf`). `AttachSource`를 다시 부르면 시퀀스 번호를 먹어 그 뒤의 번호가 달라진다.
   - 빈 데이터베이스가 필요한 테스트(`pgconn.NewTestDatabase`, `NewTestPool`, `pgstore.NewTestDatabase`, `sxOpen`, 마이그레이션과 옛 테이블의 관문 테스트)는 그대로다.
9. **복제본은 새로 만든 것과 같다.** `pgconn.DumpTestDatabase`는 시스템 밖의 모든 릴레이션(종류, `relpages`, `reltuples`), 컬럼, 인덱스, 제약, 트리거, 뷰, 함수, 통계 객체, 시퀀스 값, 모든 테이블의 모든 행, 플래너 통계의 행 수를 줄로 낸다(서버 시계로 하루 안의 시각은 `recent`로). 세 테스트가 복제본과 느린 길로 만든 데이터베이스를 이것으로 비교한다.
10. **준비가 싸진 테스트는 나란히 돈다.** 준비만 줄이면 `adapter.pgingest`는 76%에 머물렀다(남은 시간의 대부분이 데이터를 다루는 일이다). 테스트마다 자기 데이터베이스가 있으므로 도우미가 최상위 테스트를 `t.Parallel()`로 돌린다. `pgconn.TestInParallel`이 한 테스트에 한 번만 부르고(두 번 부르면 Go가 멈춘다), 하위 테스트는 건드리지 않는다(부모가 하위 테스트 뒤에 그 결과를 읽을 수 있다). 부르는 곳은 `pgiOpen`, `rdfNew`, `v2rWorldWith`, `newRig`, `sapLoadBothInto`, `sapStagedInto`, 그리고 빈 데이터베이스에서 시작하는 `sxOpen`, `igtOpenStandard`이고, 적합성 스위트 넷과 `TestV2mWritesAndFeedsFailWhenATableIsGone`은 하위 테스트를 나란히 돌린다(사례마다 저장소가 따로다). 나란히 돌지 않는 것은 목록(`pgiSerial`, `v2rSerial`, `sapSerial`, `sxSerial`)에 이름으로 둔다: 플래너가 읽는 행 수나 계획을 단언하는 테스트(계획이 ANALYZE와 autovacuum의 통계에서 오므로 함께 도는 일이 그 시점을 바꿀 수 있다), 걸린 시간을 재는 테스트, 서버가 모든 연결에 함께 쓰는 잠금 테이블을 재는 테스트, 잠금 수를 세거나 시간 한도를 두고 잠금을 기다리는 테스트. Go는 이것들을 나란히 도는 테스트보다 먼저 하나씩 돌린다. 환경 변수를 바꾸는 테스트(`t.Setenv`)는 이 도우미를 쓰지 않는다. 동시에 도는 수는 `go test`의 기본(`GOMAXPROCS`)이고 관문의 명령은 그대로다. 도우미를 쓰는 새 테스트는 기본으로 나란히 돈다. 공용 상태(환경 변수, 패키지 전역, 서버 전체의 설정이나 잠금 테이블, 플래너의 계획)를 쓰는 테스트를 더하면 목록에 이름을 올린다.

## 예외 상황과 막는 테스트

| 상황 | 막는 것 | 테스트 |
|---|---|---|
| 템플릿이 낡음 | 키가 마이그레이션, 저장 형식, 계획 문장, 매니페스트, KG 필드, `thenKey`를 담는다. 키에 없는 Go 코드의 변경은 비교 테스트가 잡는다 | `TestTplTheKeyOfAKGTemplateChangesWithEverythingThatDecidesWhatTheTemplateHolds`, `TestTplACopyOfTheMigratedTemplateIsTheDatabaseThatOpenMakesFromAnEmptyOne`(키에 모든 마이그레이션) |
| 여러 테스트·프로세스가 한꺼번에 | 뮤텍스, advisory lock, 잠근 뒤 다시 찾기(따로 된 문장이라 잠근 뒤의 스냅샷으로 찾는다) | `TestTplTestsOfOneProcessThatAskForATemplateAtOnceMakeItOnce`, `TestTplAProcessWaitsForTheOneThatMakesTheTemplateAndThenCopiesWhatItMade`, `TestTplAProcessThatStopsWaitingOrCannotReachTheServerFailsAndMakesNothing` |
| 만들다 실패, 플래너 통계가 남음 | 실패하고 아무것도 남기지 않으며, 다음 요청이 새로 만든다 | `TestTplATemplateThatCannotBeMadeFailsTheTestAndLeavesNothingBehind`, `TestTplAKGTemplateThatCannotBeMadeFailsTheTestAndIsNotMadeAnotherWay` |
| 통계가 복사됨 | 템플릿에 플래너 통계를 두지 않는다. 비교의 줄에 `relpages`, `reltuples`, 플래너 통계의 행 수가 있다. 규모 테스트는 혼자 돈다 | 위의 테스트와 비교 테스트, 규모 테스트 여섯을 세 번씩 돌린 결과(아래) |
| 나란히 돌 때 공용 상태 | 한 번만, 최상위만, 목록의 테스트는 혼자 | `TestTplATestIsMadeParallelOnceWhateverHelpersAskAndASubtestIsLeftAsItIs` |
| 복제본이 설치 토큰을 나눔 | 복제마다 토큰을 새로 낸다. KG 템플릿에 질의 스키마를 두지 않는다 | `TestTplTwoCopiesOfATemplateMakeAndDeleteKGsWithAQuerySchemaWithoutMeetingTheirRoles`, `TestTplACopyOfTheMigratedTemplateIsTheDatabaseThatOpenMakesFromAnEmptyOne`, `TestTplTheKeyOfAKGTemplateChangesWithEverythingThatDecidesWhatTheTemplateHolds`(질의 스키마 모델 거절) |
| 복제가 거절됨 | 다른 길 없이 실패 | `TestTplACopyThatTheServerRefusesFailsTheTestAndIsNotMadeAnotherWay` |
| 서버가 없음 | `NewTestDatabase`와 같이 실패, DSN을 보이지 않음 | `TestTplATemplateNeedsTheTestServerAsATestDatabaseDoes` |
| 복제본이 새로 만든 것과 다름 | 비교 | `TestTplATemplateIsMadeOnceAndEveryCopyIsADatabaseOfItsOwnWithWhatTheTemplateHas`, `TestTplACopyOfTheMigratedTemplateIsTheDatabaseThatOpenMakesFromAnEmptyOne`, `TestTplACopyOfAKGTemplateIsTheDatabaseThatOpenAndCreateKGMakeTheSlowWay`, `TestTplTheDatabaseATestOfTheReceivingStartsFromIsTheOneOpenCreateKGAndAttachSourceMakeTheSlowWay` |

## 질의 스키마와 설치 토큰 (ADR-0113)

마이그레이션 `0008_install_token`은 데이터베이스마다 무작위 설치 토큰을 만들고, 질의 스키마의 역할 이름에 그 토큰이 들어간다. 역할은 클러스터의 것이라 복제되지 않고, 같은 토큰의 데이터베이스 둘이 역할을 나누면 KG를 지울 수 없다. 그래서 셋을 지킨다.

- 복제본마다 토큰을 새로 낸다. `makeTestDatabase`가 템플릿에서 복제한 직후 `install_token`이 있으면 마이그레이션과 같은 식으로 새 12자리를 쓴다(`testRenewToken`). 실패하면 테스트가 실패한다. 비교 덤프는 토큰 값만 가린다.
- KG 템플릿은 질의 스키마가 없는 모델에만 있다. `testKGKey`가 질의 스키마를 만드는 모델을 거절하므로 템플릿에는 역할이 없다.
- 질의 스키마의 테스트(`Qsc*`)는 `sxOpen`과 `NewTestDatabase`로 빈 데이터베이스에서 시작해 복제본을 쓰지 않는다. 데이터베이스마다 마이그레이션이 만든 토큰이 다르므로 나란히 돌아도 역할이 겹치지 않는다(계획과 시간을 재는 셋은 `sxSerial`로 혼자 돈다).

복제본 둘이 각자 질의 스키마가 있는 KG를 만들고 지울 수 있음을 `TestTplTwoCopiesOfATemplateMakeAndDeleteKGsWithAQuerySchemaWithoutMeetingTheirRoles`가, 복제본과 새로 만든 데이터베이스의 토큰이 모두 다름을 `TestTplACopyOfTheMigratedTemplateIsTheDatabaseThatOpenMakesFromAnEmptyOne`이 본다.

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

`amends_tests`의 262개(`adapter.pgingest` 127, `adapter.pgstore` 135)는 단언이 하나도 바뀌지 않았다. 지문이 바뀐 이유는 셋이다.

| 바뀐 것 | 걸린 테스트 |
|---|---|
| `pgiOpen`(마이그레이션 템플릿, 나란히), `pgiEnv.kg`(KG 필드를 `pgiKG`로 뺐다), `rdfNew`(KG 템플릿, 소스 번호를 읽기만 하는 `rdfRefOf`) | `adapter.pgingest`의 이 도우미를 쓰는 모든 테스트 |
| `v2rWorldWith`(KG 템플릿, 나란히), `newRig`, `sapLoadBothInto`, `sapStagedInto`, `sxOpen`, `igtOpenStandard`(나란히) | `adapter.pgstore`의 이 도우미를 쓰는 테스트 |
| 하위 테스트의 첫 줄에 `t.Parallel()` | 적합성 스위트 넷, `TestV2mWritesAndFeedsFailWhenATableIsGone` |

요구사항은 더하지 않는다. 테스트의 준비와 실행 방식은 REQ-270(테스트는 실제 PostgreSQL에서 돌고 서버가 없으면 실패한다)과 REQ-023(테스트 잠금) 안의 구현이고, 제품의 동작은 바뀌지 않는다.

## 결과

같은 기계에서 앞뒤를 이어서 잰 시간(`go test -count=1 -cover -v`, 패키지마다, 초). 다른 작업이 함께 도는 기계라 같은 코드도 10~20% 흔들린다.

| 패키지 | 전 | 후 | 비율 |
|---|---|---|---|
| `adapter.pgingest`(ADR-0113 전) | 552 | 298 | 54% |
| `adapter.pgingest`(ADR-0113 뒤) | 600에서 끊김 | 324 | 54% 이하 |
| `adapter.pgstore`(ADR-0113 전) | 374 | 191 | 51% |
| `adapter.pgstore`(ADR-0113 뒤) | 488 | 270 | 55% |
| `adapter.pgcheck` | 12.4 | 11.5 | |
| `adapter.identitycheck` | 3.5 | 2.9 | |

- 커버리지 하한은 그대로다: `adapter.pgconn` 95.2%(하한 95), `adapter.pgstore` 92.6%(92.5), `adapter.pgingest` 92.6%(92.5).
- 플래너가 읽는 행 수를 재는 규모 테스트 여섯은 혼자 돌고, 세 번씩 돌려 모두 통과했다(ADR-0113 전과 뒤에 각각). 패키지 전체를 돌린 때마다 함께 통과했다.
- 템플릿은 `adapter.pgingest`의 실행 하나에 일곱 개(KG 템플릿 21~22MB 여섯과 마이그레이션 템플릿 8MB, 약 134MB)가 서버에 남는다. 서버를 버릴 때 함께 사라진다.
- 새로 넣은 테스트의 준비 시간은 테스트 수에 곱해지지 않는다: 같은 준비의 테스트는 복제 한 번(0.2초 안팎)만 더한다.
