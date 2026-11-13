package graphcheck_test

import (
	"testing"

	"github.com/ysk6506/praxium/backend/internal/graph"
	"github.com/ysk6506/praxium/backend/internal/graphcheck"
	"github.com/ysk6506/praxium/backend/internal/memstore"
)

// [REQ-132] [REQ-135] [REQ-246] [REQ-247] [REQ-248] [REQ-249]
func TestReferenceStorePassesTheSuite(t *testing.T) {
	// Every case of the suite, in order, with the requirement it shows. The
	// subtest name carries the tag, so each case answers for one requirement
	// and a case that is missing here fails the test.
	tagged := []struct{ name, req string }{
		{"apply_read_round_trip", "[REQ-132]"},
		{"apply_no_change_makes_no_change_number", "[REQ-135]"},
		{"apply_rejects_invalid_batches_without_trace", "[REQ-132]"},
		{"apply_same_input_same_hashes_in_two_stores", "[REQ-246]"},
		{"apply_cardinality_and_dangling_links", "[REQ-132]"},
		{"chunked_apply_is_invisible_until_the_last_chunk", "[REQ-249]"},
		{"chunked_apply_abandon_reverts_and_reuses_numbers", "[REQ-249]"},
		{"chunked_apply_resumes_after_a_restart", "[REQ-249]"},
		{"chunked_apply_refuses_other_work_while_open", "[REQ-249]"},
		{"apply_retry_of_a_finished_apply_is_exactly_once", "[REQ-249]"},
		{"write_with_a_stale_expected_head_is_refused", "[REQ-132]"},
		{"first_load_serves_only_after_the_last_chunk", "[REQ-249]"},
		{"lease_is_exclusive_until_it_expires_or_is_released", "[REQ-249]"},
		{"lease_refuses_a_lower_semantic_version", "[REQ-246]"},
		{"lease_renewal_keeps_a_long_apply_alive", "[REQ-249]"},
		{"write_after_the_lease_expired_is_refused", "[REQ-249]"},
		{"identifiers_are_looked_up_and_keep_their_history", "[REQ-135]"},
		{"identifier_naming_two_objects_in_one_scope_is_refused", "[REQ-135]"},
		{"identity_events_are_recorded_and_not_rewound", "[REQ-135]"},
		{"time_travel_reads_any_past_change_number", "[REQ-135]"},
		{"volatile_values_are_in_latest_reads_only", "[REQ-247]"},
		{"scans_page_in_id_order_and_links_are_found_from_both_ends", "[REQ-135]"},
		{"kgs_are_found_by_id_name_and_main_and_do_not_mix", "[REQ-135]"},
		{"restore_raises_the_generation_and_old_readers_are_told", "[REQ-248]"},
		{"one_applier_and_many_readers_see_whole_states", "[REQ-132]"},
		{"applied_change_feed_follows_from_a_position", "[REQ-135]"},
		{"light_change_feed_carries_volatile_changes_only", "[REQ-247]"},
		{"wait_readable_waits_for_the_change_number", "[REQ-249]"},
		{"seal_makes_a_version_only_when_the_hash_changed", "[REQ-135]"},
		{"a_sealed_version_reads_its_own_state", "[REQ-135]"},
		{"a_version_carries_the_closed_input_list", "[REQ-246]"},
	}
	cases := graphcheck.Cases()
	if len(cases) != len(tagged) {
		t.Fatalf("the suite has %d cases, the table of this test names %d", len(cases), len(tagged))
	}
	factory := func(graphcheck.TB) graph.Store { return memstore.New() }
	for i, c := range cases {
		if c.Name != tagged[i].name {
			t.Fatalf("case %d is %q, the table expects %q", i, c.Name, tagged[i].name)
		}
		t.Run(tagged[i].req+" "+c.Name, func(t *testing.T) { c.Run(t, factory) })
	}
}

// The names and the order of the cases are fixed: a case that goes away, comes
// back under another name or moves is a change of the suite that adapters are
// held to, and it is made on purpose with an amendment of the lock.
//
// [REQ-249]
func TestSuiteCaseNamesAreFixed(t *testing.T) {
	want := "apply_read_round_trip,apply_no_change_makes_no_change_number," +
		"apply_rejects_invalid_batches_without_trace,apply_same_input_same_hashes_in_two_stores," +
		"apply_cardinality_and_dangling_links,chunked_apply_is_invisible_until_the_last_chunk," +
		"chunked_apply_abandon_reverts_and_reuses_numbers,chunked_apply_resumes_after_a_restart," +
		"chunked_apply_refuses_other_work_while_open,apply_retry_of_a_finished_apply_is_exactly_once," +
		"write_with_a_stale_expected_head_is_refused,first_load_serves_only_after_the_last_chunk," +
		"lease_is_exclusive_until_it_expires_or_is_released,lease_refuses_a_lower_semantic_version," +
		"lease_renewal_keeps_a_long_apply_alive,write_after_the_lease_expired_is_refused," +
		"identifiers_are_looked_up_and_keep_their_history,identifier_naming_two_objects_in_one_scope_is_refused," +
		"identity_events_are_recorded_and_not_rewound,time_travel_reads_any_past_change_number," +
		"volatile_values_are_in_latest_reads_only,scans_page_in_id_order_and_links_are_found_from_both_ends," +
		"kgs_are_found_by_id_name_and_main_and_do_not_mix,restore_raises_the_generation_and_old_readers_are_told," +
		"one_applier_and_many_readers_see_whole_states,applied_change_feed_follows_from_a_position," +
		"light_change_feed_carries_volatile_changes_only,wait_readable_waits_for_the_change_number," +
		"seal_makes_a_version_only_when_the_hash_changed,a_sealed_version_reads_its_own_state," +
		"a_version_carries_the_closed_input_list"
	got := ""
	for i, c := range graphcheck.Cases() {
		if i > 0 {
			got += ","
		}
		got += c.Name
	}
	if got != want {
		t.Fatalf("the cases of the suite changed:\n got %s\nwant %s", got, want)
	}
}
