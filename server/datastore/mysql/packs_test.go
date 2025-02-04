package mysql

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/WatchBeam/clock"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/ptr"
	"github.com/fleetdm/fleet/v4/server/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeletePack(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	pack := test.NewPack(t, ds, "foo")
	assert.NotEqual(t, uint(0), pack.ID)

	pack, err := ds.Pack(context.Background(), pack.ID)
	require.Nil(t, err)

	err = ds.DeletePack(context.Background(), pack.Name)
	assert.Nil(t, err)

	assert.NotEqual(t, uint(0), pack.ID)
	pack, err = ds.Pack(context.Background(), pack.ID)
	assert.NotNil(t, err)
}

func TestSavePack(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	expectedPack := &fleet.Pack{
		Name:     "foo",
		HostIDs:  []uint{1},
		LabelIDs: []uint{1},
		TeamIDs:  []uint{1},
	}

	pack, err := ds.NewPack(context.Background(), expectedPack)
	require.NoError(t, err)
	assert.NotEqual(t, uint(0), pack.ID)
	test.EqualSkipTimestampsID(t, expectedPack, pack)

	pack, err = ds.Pack(context.Background(), pack.ID)
	require.NoError(t, err)
	test.EqualSkipTimestampsID(t, expectedPack, pack)

	expectedPack = &fleet.Pack{
		ID:       pack.ID,
		Name:     "bar",
		HostIDs:  []uint{3},
		LabelIDs: []uint{4, 6},
		TeamIDs:  []uint{},
	}

	err = ds.SavePack(context.Background(), expectedPack)
	require.NoError(t, err)

	pack, err = ds.Pack(context.Background(), pack.ID)
	require.NoError(t, err)
	assert.Equal(t, "bar", pack.Name)
	test.EqualSkipTimestampsID(t, expectedPack, pack)
}

func TestGetPackByName(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	pack := test.NewPack(t, ds, "foo")
	assert.NotEqual(t, uint(0), pack.ID)

	pack, ok, err := ds.PackByName(context.Background(), pack.Name)
	require.Nil(t, err)
	assert.True(t, ok)
	assert.NotNil(t, pack)
	assert.Equal(t, "foo", pack.Name)

	pack, ok, err = ds.PackByName(context.Background(), "bar")
	require.Nil(t, err)
	assert.False(t, ok)
	assert.Nil(t, pack)

}

func TestListPacks(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	p1 := &fleet.PackSpec{
		ID:   1,
		Name: "foo_pack",
	}
	p2 := &fleet.PackSpec{
		ID:   2,
		Name: "bar_pack",
	}
	err := ds.ApplyPackSpecs(context.Background(), []*fleet.PackSpec{p1})
	require.Nil(t, err)

	packs, err := ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: false})
	require.Nil(t, err)
	assert.Len(t, packs, 1)

	err = ds.ApplyPackSpecs(context.Background(), []*fleet.PackSpec{p1, p2})
	require.Nil(t, err)

	packs, err = ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: false})
	require.Nil(t, err)
	assert.Len(t, packs, 2)
}

func setupPackSpecsTest(t *testing.T, ds fleet.Datastore) []*fleet.PackSpec {
	zwass := test.NewUser(t, ds, "Zach", "zwass@example.com", true)
	queries := []*fleet.Query{
		{Name: "foo", Description: "get the foos", Query: "select * from foo"},
		{Name: "bar", Description: "do some bars", Query: "select baz from bar"},
	}
	// Zach creates some queries
	err := ds.ApplyQueries(context.Background(), zwass.ID, queries)
	require.Nil(t, err)

	labels := []*fleet.LabelSpec{
		{
			Name:  "foo",
			Query: "select * from foo",
		},
		{
			Name:  "bar",
			Query: "select * from bar",
		},
		{
			Name:  "bing",
			Query: "select * from bing",
		},
	}
	err = ds.ApplyLabelSpecs(context.Background(), labels)
	require.Nil(t, err)

	expectedSpecs := []*fleet.PackSpec{
		{
			ID:   1,
			Name: "test_pack",
			Targets: fleet.PackSpecTargets{
				Labels: []string{
					"foo",
					"bar",
					"bing",
				},
			},
			Queries: []fleet.PackSpecQuery{
				{
					QueryName:   queries[0].Name,
					Name:        "q0",
					Description: "test_foo",
					Interval:    42,
				},
				{
					QueryName: queries[0].Name,
					Name:      "foo_snapshot",
					Interval:  600,
					Snapshot:  ptr.Bool(true),
					Denylist:  ptr.Bool(false),
				},
				{
					Name:      "q2",
					QueryName: queries[1].Name,
					Interval:  600,
					Removed:   ptr.Bool(false),
					Shard:     ptr.Uint(73),
					Platform:  ptr.String("foobar"),
					Version:   ptr.String("0.0.0.0.0.1"),
					Denylist:  ptr.Bool(true),
				},
			},
		},
		{
			ID:       2,
			Name:     "test_pack_disabled",
			Disabled: true,
			Targets: fleet.PackSpecTargets{
				Labels: []string{
					"foo",
					"bar",
					"bing",
				},
			},
			Queries: []fleet.PackSpecQuery{
				{
					QueryName:   queries[0].Name,
					Name:        "q0",
					Description: "test_foo",
					Interval:    42,
				},
				{
					QueryName: queries[0].Name,
					Name:      "foo_snapshot",
					Interval:  600,
					Snapshot:  ptr.Bool(true),
				},
				{
					Name:      "q2",
					QueryName: queries[1].Name,
					Interval:  600,
					Removed:   ptr.Bool(false),
					Shard:     ptr.Uint(73),
					Platform:  ptr.String("foobar"),
					Version:   ptr.String("0.0.0.0.0.1"),
				},
			},
		},
	}

	err = ds.ApplyPackSpecs(context.Background(), expectedSpecs)
	require.Nil(t, err)
	return expectedSpecs
}

func TestApplyPackSpecRoundtrip(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	expectedSpecs := setupPackSpecsTest(t, ds)

	gotSpec, err := ds.GetPackSpecs(context.Background())
	require.Nil(t, err)
	assert.Equal(t, expectedSpecs, gotSpec)
}

func TestGetPackSpec(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	expectedSpecs := setupPackSpecsTest(t, ds)

	for _, s := range expectedSpecs {
		spec, err := ds.GetPackSpec(context.Background(), s.Name)
		require.Nil(t, err)
		assert.Equal(t, s, spec)
	}
}

func TestApplyPackSpecMissingQueries(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	// Do not define queries mentioned in spec
	specs := []*fleet.PackSpec{
		{
			ID:   1,
			Name: "test_pack",
			Targets: fleet.PackSpecTargets{
				Labels: []string{},
			},
			Queries: []fleet.PackSpecQuery{
				{
					QueryName: "bar",
					Interval:  600,
				},
			},
		},
	}

	// Should error due to unkown query
	err := ds.ApplyPackSpecs(context.Background(), specs)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "unknown query 'bar'")
	}
}

func TestApplyPackSpecMissingName(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	setupPackSpecsTest(t, ds)

	specs := []*fleet.PackSpec{
		{
			Name: "test2",
			Targets: fleet.PackSpecTargets{
				Labels: []string{},
			},
			Queries: []fleet.PackSpecQuery{
				{
					QueryName: "foo",
					Interval:  600,
				},
			},
		},
	}
	err := ds.ApplyPackSpecs(context.Background(), specs)
	require.NoError(t, err)

	// Query name should have been copied into name field
	spec, err := ds.GetPackSpec(context.Background(), "test2")
	require.NoError(t, err)
	assert.Equal(t, "foo", spec.Queries[0].Name)
}

func TestListPacksForHost(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	mockClock := clock.NewMockClock()

	l1 := &fleet.LabelSpec{
		ID:   1,
		Name: "foo",
	}
	l2 := &fleet.LabelSpec{
		ID:   2,
		Name: "bar",
	}
	err := ds.ApplyLabelSpecs(context.Background(), []*fleet.LabelSpec{l1, l2})
	require.Nil(t, err)

	p1 := &fleet.PackSpec{
		ID:   1,
		Name: "foo_pack",
		Targets: fleet.PackSpecTargets{
			Labels: []string{
				l1.Name,
				l2.Name,
			},
		},
	}
	p2 := &fleet.PackSpec{
		ID:   2,
		Name: "shmoo_pack",
		Targets: fleet.PackSpecTargets{
			Labels: []string{
				l2.Name,
			},
		},
	}
	err = ds.ApplyPackSpecs(context.Background(), []*fleet.PackSpec{p1, p2})
	require.Nil(t, err)

	h1 := test.NewHost(t, ds, "h1.local", "10.10.10.1", "1", "1", mockClock.Now())

	packs, err := ds.ListPacksForHost(context.Background(), h1.ID)
	require.Nil(t, err)
	require.Len(t, packs, 0)

	err = ds.RecordLabelQueryExecutions(
		context.Background(),
		h1,
		map[uint]*bool{l1.ID: ptr.Bool(true)},
		mockClock.Now(),
	)
	require.Nil(t, err)

	packs, err = ds.ListPacksForHost(context.Background(), h1.ID)
	require.Nil(t, err)
	if assert.Len(t, packs, 1) {
		assert.Equal(t, "foo_pack", packs[0].Name)
	}

	err = ds.RecordLabelQueryExecutions(
		context.Background(),
		h1,
		map[uint]*bool{l1.ID: ptr.Bool(false), l2.ID: ptr.Bool(true)},
		mockClock.Now(),
	)
	require.Nil(t, err)

	packs, err = ds.ListPacksForHost(context.Background(), h1.ID)
	require.Nil(t, err)
	assert.Len(t, packs, 2)

	err = ds.RecordLabelQueryExecutions(
		context.Background(),
		h1,
		map[uint]*bool{l1.ID: ptr.Bool(true), l2.ID: ptr.Bool(true)},
		mockClock.Now(),
	)
	require.Nil(t, err)

	packs, err = ds.ListPacksForHost(context.Background(), h1.ID)
	require.Nil(t, err)
	assert.Len(t, packs, 2)

	h2 := test.NewHost(t, ds, "h2.local", "10.10.10.2", "2", "2", mockClock.Now())

	err = ds.RecordLabelQueryExecutions(
		context.Background(),
		h2,
		map[uint]*bool{l2.ID: ptr.Bool(true)},
		mockClock.Now(),
	)
	require.Nil(t, err)

	packs, err = ds.ListPacksForHost(context.Background(), h1.ID)
	require.Nil(t, err)
	assert.Len(t, packs, 2)

	err = ds.RecordLabelQueryExecutions(
		context.Background(),
		h1,
		map[uint]*bool{l2.ID: ptr.Bool(false)},
		mockClock.Now(),
	)
	require.Nil(t, err)

	packs, err = ds.ListPacksForHost(context.Background(), h1.ID)
	require.Nil(t, err)
	if assert.Len(t, packs, 1) {
		assert.Equal(t, "foo_pack", packs[0].Name)
	}
}

func TestEnsureGlobalPack(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	test.AddAllHostsLabel(t, ds)

	packs, err := ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: true})
	require.Nil(t, err)
	assert.Len(t, packs, 0)

	gp, err := ds.EnsureGlobalPack(context.Background())
	require.Nil(t, err)

	packs, err = ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: true})
	require.Nil(t, err)
	assert.Len(t, packs, 1)
	assert.Equal(t, gp.ID, packs[0].ID)
	assert.Equal(t, "global", *gp.Type)

	labels, err := ds.LabelIDsByName(context.Background(), []string{"All Hosts"})
	require.Nil(t, err)

	assert.Equal(t, []uint{labels[0]}, gp.LabelIDs)

	_, err = ds.EnsureGlobalPack(context.Background())
	require.Nil(t, err)

	packs, err = ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: true})
	require.Nil(t, err)
	assert.Len(t, packs, 1)
	assert.Equal(t, gp.ID, packs[0].ID)
	assert.Equal(t, "global", *gp.Type)
}

func TestEnsureTeamPack(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	packs, err := ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: true})
	require.Nil(t, err)
	assert.Len(t, packs, 0)

	_, err = ds.EnsureTeamPack(context.Background(), 12)
	require.Error(t, err)

	team1, err := ds.NewTeam(context.Background(), &fleet.Team{Name: "team1"})
	require.NoError(t, err)

	tp, err := ds.EnsureTeamPack(context.Background(), team1.ID)
	require.NoError(t, err)

	packs, err = ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: true})
	require.Nil(t, err)
	assert.Len(t, packs, 1)
	assert.Equal(t, tp.ID, packs[0].ID)
	assert.Equal(t, teamScheduleName(team1), tp.Name)
	assert.Equal(t, fmt.Sprintf("team-%d", team1.ID), *tp.Type)
	assert.Equal(t, []uint{team1.ID}, tp.TeamIDs)

	_, err = ds.EnsureTeamPack(context.Background(), team1.ID)
	require.NoError(t, err)

	packs, err = ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: true})
	require.Nil(t, err)
	assert.Len(t, packs, 1)
	assert.Equal(t, tp.ID, packs[0].ID)

	team2, err := ds.NewTeam(context.Background(), &fleet.Team{Name: "team2"})
	require.NoError(t, err)

	tp2, err := ds.EnsureTeamPack(context.Background(), team2.ID)
	require.NoError(t, err)

	packs, err = ds.ListPacks(context.Background(), fleet.PackListOptions{IncludeSystemPacks: true})
	require.Nil(t, err)
	assert.Len(t, packs, 2)
	assert.Equal(t, tp.ID, packs[0].ID)
	assert.Equal(t, tp2.ID, packs[1].ID)

	assert.Equal(t, fmt.Sprintf("team-%d", team2.ID), *tp2.Type)
	assert.Equal(t, []uint{team2.ID}, tp2.TeamIDs)
}

func TestTeamNameChangesTeamSchedule(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	team1, err := ds.NewTeam(context.Background(), &fleet.Team{Name: "team1"})
	require.NoError(t, err)

	tp, err := ds.EnsureTeamPack(context.Background(), team1.ID)
	require.NoError(t, err)
	firstName := teamScheduleName(team1)
	assert.Equal(t, firstName, tp.Name)

	team1.Name = "new name!!"
	team1, err = ds.SaveTeam(context.Background(), team1)
	require.NoError(t, err)

	tp, err = ds.EnsureTeamPack(context.Background(), team1.ID)
	require.NoError(t, err)
	assert.NotEqual(t, firstName, tp.Name)
	assert.Equal(t, teamScheduleName(team1), tp.Name)
}

func TestTeamScheduleNamesMigrateToNewFormat(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	team1, err := ds.NewTeam(context.Background(), &fleet.Team{Name: "team1"})
	require.NoError(t, err)

	// insert team pack by hand with the old naming scheme
	_, err = ds.writer.Exec(
		"INSERT INTO packs(name, description, platform, disabled, pack_type) VALUES (?, ?, ?, ?, ?)",
		teamSchedulePackType(team1), "desc", "windows", false, teamSchedulePackType(team1),
	)
	require.NoError(t, err)

	tp, err := ds.EnsureTeamPack(context.Background(), team1.ID)
	require.NoError(t, err)
	require.Equal(t, teamSchedulePackType(team1), tp.Name)

	require.NoError(t, ds.MigrateData(context.Background()))

	tp, err = ds.EnsureTeamPack(context.Background(), team1.ID)
	require.NoError(t, err)
	require.NotEqual(t, teamSchedulePackType(team1), tp.Name)
	require.Equal(t, teamScheduleName(team1), tp.Name)
}

func TestApplyPackSpecFailsOnTargetIDNull(t *testing.T) {
	ds := CreateMySQLDS(t)
	defer ds.Close()

	// Do not define queries mentioned in spec
	specs := []*fleet.PackSpec{
		{
			ID:   1,
			Name: "test_pack",
			Targets: fleet.PackSpecTargets{
				Labels: []string{"UnexistentLabel"},
			},
		},
	}

	// Should error due to unkown label target id
	err := ds.ApplyPackSpecs(context.Background(), specs)
	require.Error(t, err)
}

func randomPackStatsForHost(hostID, packID uint, scheduledQueries []*fleet.ScheduledQuery) *fleet.Host {
	var queryStats []fleet.ScheduledQueryStats

	amount := rand.Intn(5000)

	for i := 0; i < amount; i++ {
		sq := scheduledQueries[rand.Intn(len(scheduledQueries))]
		queryStats = append(queryStats, fleet.ScheduledQueryStats{
			ScheduledQueryName: sq.Name,
			ScheduledQueryID:   sq.ID,
			QueryName:          sq.QueryName,
			Description:        sq.Description,
			PackID:             packID,
			AverageMemory:      rand.Intn(100),
			Denylisted:         false,
			Executions:         rand.Intn(100),
			Interval:           rand.Intn(100),
			LastExecuted:       time.Now(),
			OutputSize:         rand.Intn(1000),
			SystemTime:         rand.Intn(1000),
			UserTime:           rand.Intn(1000),
			WallTime:           rand.Intn(1000),
		})
	}
	return &fleet.Host{
		ID: hostID,
		PackStats: []fleet.PackStats{
			{
				PackID:     packID,
				QueryStats: queryStats,
			},
		},
	}
}

func TestPackApplyStatsNotLocking(t *testing.T) {
	t.Skip("This can be too much for the test db if you're running all tests")

	ds := CreateMySQLDS(t)
	defer ds.Close()

	specs := setupPackSpecsTest(t, ds)

	host, err := ds.NewHost(context.Background(), &fleet.Host{
		DetailUpdatedAt: time.Now(),
		LabelUpdatedAt:  time.Now(),
		SeenTime:        time.Now(),
		NodeKey:         "1",
		UUID:            "1",
		Hostname:        "foo.local",
		PrimaryIP:       "192.168.1.1",
		PrimaryMac:      "30-65-EC-6F-C4-58",
	})
	require.NoError(t, err)
	require.NotNil(t, host)

	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pack, _, err := ds.PackByName(context.Background(), "test_pack")
				require.NoError(t, err)
				schedQueries, err := ds.ListScheduledQueriesInPack(context.Background(), pack.ID, fleet.ListOptions{})
				require.NoError(t, err)

				require.NoError(t, saveHostPackStatsDB(context.Background(), ds.writer, randomPackStatsForHost(host.ID, pack.ID, schedQueries)))
			}
		}
	}()

	time.Sleep(1 * time.Second)
	for i := 0; i < 1000; i++ {
		require.NoError(t, ds.ApplyPackSpecs(context.Background(), specs))
		time.Sleep(77 * time.Millisecond)
	}

	cancelFunc()
}

func TestPackApplyStatsNotLockingTryTwo(t *testing.T) {
	t.Skip("This can be too much for the test db if you're running all tests")

	ds := CreateMySQLDS(t)
	defer ds.Close()

	setupPackSpecsTest(t, ds)

	host, err := ds.NewHost(context.Background(), &fleet.Host{
		DetailUpdatedAt: time.Now(),
		LabelUpdatedAt:  time.Now(),
		SeenTime:        time.Now(),
		NodeKey:         "1",
		UUID:            "1",
		Hostname:        "foo.local",
		PrimaryIP:       "192.168.1.1",
		PrimaryMac:      "30-65-EC-6F-C4-58",
	})
	require.NoError(t, err)
	require.NotNil(t, host)

	ctx, cancelFunc := context.WithCancel(context.Background())
	for i := 0; i < 2; i++ {
		go func() {
			ms := rand.Intn(100)
			if ms == 0 {
				ms = 10
			}
			ticker := time.NewTicker(time.Duration(ms) * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					pack, _, err := ds.PackByName(context.Background(), "test_pack")
					require.NoError(t, err)
					schedQueries, err := ds.ListScheduledQueriesInPack(context.Background(), pack.ID, fleet.ListOptions{})
					require.NoError(t, err)

					require.NoError(t, saveHostPackStatsDB(context.Background(), ds.writer, randomPackStatsForHost(host.ID, pack.ID, schedQueries)))
				}
			}
		}()
	}

	time.Sleep(60 * time.Second)

	cancelFunc()
}
