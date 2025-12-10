package provisionerdserver

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogtest"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbmock"
	"github.com/coder/coder/v2/coderd/database/dbtime"
	"github.com/coder/coder/v2/coderd/telemetry"
	"github.com/coder/coder/v2/codersdk"
	sdkproto "github.com/coder/coder/v2/provisionersdk/proto"
	"github.com/coder/serpent"
)

// TestAgentMetadataMinInterval_TemplateImportStrict tests that InsertWorkspaceResource
// fails when ValidationModeStrict is used and metadata intervals are below the minimum.
func TestAgentMetadataMinInterval_TemplateImportStrict(t *testing.T) {
	t.Parallel()

	t.Run("FailsWhenBelowMinimum", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		db := dbmock.NewMockStore(ctrl)
		logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)

		// Setup deployment values with 60s minimum
		deploymentValues := &codersdk.DeploymentValues{}
		deploymentValues.AgentMetadataMinInterval = serpent.Duration(60 * time.Second)

		jobID := uuid.New()
		resourceID := uuid.New()
		agentID := uuid.New()

		// Mock successful resource insertion
		db.EXPECT().
			InsertWorkspaceResource(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceResource{
				ID:    resourceID,
				JobID: jobID,
			}, nil)

		// Mock agent insertion (happens before validation)
		db.EXPECT().
			InsertWorkspaceAgent(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceAgent{
				ID:         agentID,
				ResourceID: resourceID,
				Name:       "main",
				CreatedAt:  dbtime.Now(),
			}, nil)

		// Create resource with agent that has metadata interval below minimum
		resource := &sdkproto.Resource{
			Name: "example",
			Type: "aws_instance",
			Agents: []*sdkproto.Agent{
				{
					Name: "main",
					Auth: &sdkproto.Agent_Token{},
					Metadata: []*sdkproto.Agent_Metadata{
						{
							Key:         "cpu",
							DisplayName: "CPU Usage",
							Script:      "echo 50",
							Interval:    30, // 30 seconds - below the 60s minimum
							Timeout:     5,
						},
					},
				},
			},
		}

		// Should fail with ValidationModeStrict
		err := InsertWorkspaceResource(
			ctx,
			db,
			jobID,
			database.WorkspaceTransitionStart,
			resource,
			&telemetry.Snapshot{},
			InsertWorkspaceResourceWithValidationMode(ValidationModeStrict),
			InsertWorkspaceResourceWithDeploymentValues(deploymentValues),
			InsertWorkspaceResourceWithLogger(logger),
		)

		require.Error(t, err)
		require.ErrorContains(t, err, `agent "main" metadata "cpu" interval 30s is below minimum required 60s`)
	})

	t.Run("SucceedsWhenAtOrAboveMinimum", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		db := dbmock.NewMockStore(ctrl)
		logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)

		// Setup deployment values with 60s minimum
		deploymentValues := &codersdk.DeploymentValues{}
		deploymentValues.AgentMetadataMinInterval = serpent.Duration(60 * time.Second)

		jobID := uuid.New()
		resourceID := uuid.New()
		agentID := uuid.New()

		// Mock successful resource insertion
		db.EXPECT().
			InsertWorkspaceResource(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceResource{
				ID:    resourceID,
				JobID: jobID,
			}, nil)

		// Mock agent insertion
		db.EXPECT().
			InsertWorkspaceAgent(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceAgent{
				ID:         agentID,
				ResourceID: resourceID,
				Name:       "main",
				CreatedAt:  dbtime.Now(),
			}, nil)

		// Mock log sources insertion (happens after agent)
		db.EXPECT().
			InsertWorkspaceAgentLogSources(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentLogSource{}, nil)

		// Mock scripts insertion
		db.EXPECT().
			InsertWorkspaceAgentScripts(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentScript{}, nil)

		// Mock resource metadata insertion
		db.EXPECT().
			InsertWorkspaceResourceMetadata(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceResourceMetadatum{}, nil)

		// Mock metadata insertion - should be called twice
		// Note: We use gomock.Any() for params because agent IDs are generated at runtime
		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// Verify the interval is correct (at minimum)
				require.Equal(t, int64(60), params.Interval, "Expected interval to be at minimum (60s)")
				require.Equal(t, "cpu", params.Key)
				return nil
			})

		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// Verify the interval is correct (above minimum)
				require.Equal(t, int64(120), params.Interval, "Expected interval to be preserved (120s)")
				require.Equal(t, "memory", params.Key)
				return nil
			})

		// Create resource with agent that has metadata at and above minimum
		resource := &sdkproto.Resource{
			Name: "example",
			Type: "aws_instance",
			Agents: []*sdkproto.Agent{
				{
					Name: "main",
					Auth: &sdkproto.Agent_Token{},
					Metadata: []*sdkproto.Agent_Metadata{
						{
							Key:         "cpu",
							DisplayName: "CPU Usage",
							Script:      "echo 50",
							Interval:    60, // At minimum
							Timeout:     5,
						},
						{
							Key:         "memory",
							DisplayName: "Memory Usage",
							Script:      "echo 2048",
							Interval:    120, // Above minimum
							Timeout:     5,
						},
					},
				},
			},
		}

		// Should succeed
		err := InsertWorkspaceResource(
			ctx,
			db,
			jobID,
			database.WorkspaceTransitionStart,
			resource,
			&telemetry.Snapshot{},
			InsertWorkspaceResourceWithValidationMode(ValidationModeStrict),
			InsertWorkspaceResourceWithDeploymentValues(deploymentValues),
			InsertWorkspaceResourceWithLogger(logger),
		)

		require.NoError(t, err)
	})

	t.Run("NoEnforcementWhenMinimumIsZero", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		db := dbmock.NewMockStore(ctrl)
		logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)

		// Setup deployment values with 0 minimum (disabled)
		deploymentValues := &codersdk.DeploymentValues{}
		deploymentValues.AgentMetadataMinInterval = serpent.Duration(0)

		jobID := uuid.New()
		resourceID := uuid.New()
		agentID := uuid.New()

		// Mock successful resource insertion
		db.EXPECT().
			InsertWorkspaceResource(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceResource{
				ID:    resourceID,
				JobID: jobID,
			}, nil)

		// Mock agent insertion
		db.EXPECT().
			InsertWorkspaceAgent(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceAgent{
				ID:         agentID,
				ResourceID: resourceID,
				Name:       "main",
				CreatedAt:  dbtime.Now(),
			}, nil)

		// Mock log sources insertion (happens after agent)
		db.EXPECT().
			InsertWorkspaceAgentLogSources(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentLogSource{}, nil)

		// Mock scripts insertion
		db.EXPECT().
			InsertWorkspaceAgentScripts(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentScript{}, nil)

		// Mock resource metadata insertion
		db.EXPECT().
			InsertWorkspaceResourceMetadata(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceResourceMetadatum{}, nil)

		// Mock metadata insertion - original interval should be preserved
		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// Verify the interval is preserved (1s)
				require.Equal(t, int64(1), params.Interval, "Expected interval to be preserved (1s)")
				require.Equal(t, "cpu", params.Key)
				return nil
			})

		// Create resource with very low interval
		resource := &sdkproto.Resource{
			Name: "example",
			Type: "aws_instance",
			Agents: []*sdkproto.Agent{
				{
					Name: "main",
					Auth: &sdkproto.Agent_Token{},
					Metadata: []*sdkproto.Agent_Metadata{
						{
							Key:         "cpu",
							DisplayName: "CPU Usage",
							Script:      "echo 50",
							Interval:    1, // Very low
							Timeout:     5,
						},
					},
				},
			},
		}

		// Should succeed since enforcement is disabled
		err := InsertWorkspaceResource(
			ctx,
			db,
			jobID,
			database.WorkspaceTransitionStart,
			resource,
			&telemetry.Snapshot{},
			InsertWorkspaceResourceWithValidationMode(ValidationModeStrict),
			InsertWorkspaceResourceWithDeploymentValues(deploymentValues),
			InsertWorkspaceResourceWithLogger(logger),
		)

		require.NoError(t, err)
	})
}

// TestAgentMetadataMinInterval_WorkspaceBuildUpgrade tests that InsertWorkspaceResource
// silently upgrades intervals when ValidationModeUpgrade is used.
func TestAgentMetadataMinInterval_WorkspaceBuildUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("SilentlyUpgradesWhenBelowMinimum", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		db := dbmock.NewMockStore(ctrl)
		logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)

		// Setup deployment values with 60s minimum
		deploymentValues := &codersdk.DeploymentValues{}
		deploymentValues.AgentMetadataMinInterval = serpent.Duration(60 * time.Second)

		jobID := uuid.New()
		resourceID := uuid.New()
		agentID := uuid.New()

		// Mock successful resource insertion
		db.EXPECT().
			InsertWorkspaceResource(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceResource{
				ID:    resourceID,
				JobID: jobID,
			}, nil)

		// Mock agent insertion
		db.EXPECT().
			InsertWorkspaceAgent(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceAgent{
				ID:         agentID,
				ResourceID: resourceID,
				Name:       "main",
				CreatedAt:  dbtime.Now(),
			}, nil)

		// Mock log sources insertion (happens after agent)
		db.EXPECT().
			InsertWorkspaceAgentLogSources(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentLogSource{}, nil)

		// Mock scripts insertion
		db.EXPECT().
			InsertWorkspaceAgentScripts(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentScript{}, nil)

		// Mock resource metadata insertion
		db.EXPECT().
			InsertWorkspaceResourceMetadata(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceResourceMetadatum{}, nil)

		// Mock metadata insertion - both should be upgraded to 60s
		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// Verify interval is upgraded to minimum (60s)
				require.Equal(t, int64(60), params.Interval, "Expected interval to be upgraded to 60s")
				require.Equal(t, "cpu", params.Key)
				return nil
			})

		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// Verify interval is upgraded to minimum (60s)
				require.Equal(t, int64(60), params.Interval, "Expected interval to be upgraded to 60s")
				require.Equal(t, "memory", params.Key)
				return nil
			})

		// Create resource with intervals below minimum
		resource := &sdkproto.Resource{
			Name: "example",
			Type: "aws_instance",
			Agents: []*sdkproto.Agent{
				{
					Name: "main",
					Auth: &sdkproto.Agent_Token{},
					Metadata: []*sdkproto.Agent_Metadata{
						{
							Key:         "cpu",
							DisplayName: "CPU Usage",
							Script:      "echo 50",
							Interval:    30, // Below minimum
							Timeout:     5,
						},
						{
							Key:         "memory",
							DisplayName: "Memory Usage",
							Script:      "echo 2048",
							Interval:    10, // Below minimum
							Timeout:     5,
						},
					},
				},
			},
		}

		// Should succeed with ValidationModeUpgrade (not fail)
		err := InsertWorkspaceResource(
			ctx,
			db,
			jobID,
			database.WorkspaceTransitionStart,
			resource,
			&telemetry.Snapshot{},
			InsertWorkspaceResourceWithValidationMode(ValidationModeUpgrade),
			InsertWorkspaceResourceWithDeploymentValues(deploymentValues),
			InsertWorkspaceResourceWithLogger(logger),
		)

		require.NoError(t, err)
	})

	t.Run("PreservesIntervalsAtOrAboveMinimum", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		db := dbmock.NewMockStore(ctrl)
		logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)

		// Setup deployment values with 30s minimum
		deploymentValues := &codersdk.DeploymentValues{}
		deploymentValues.AgentMetadataMinInterval = serpent.Duration(30 * time.Second)

		jobID := uuid.New()
		resourceID := uuid.New()
		agentID := uuid.New()

		// Mock successful resource insertion
		db.EXPECT().
			InsertWorkspaceResource(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceResource{
				ID:    resourceID,
				JobID: jobID,
			}, nil)

		// Mock agent insertion
		db.EXPECT().
			InsertWorkspaceAgent(gomock.Any(), gomock.Any()).
			Return(database.WorkspaceAgent{
				ID:         agentID,
				ResourceID: resourceID,
				Name:       "main",
				CreatedAt:  dbtime.Now(),
			}, nil)

		// Mock log sources insertion (happens after agent)
		db.EXPECT().
			InsertWorkspaceAgentLogSources(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentLogSource{}, nil)

		// Mock scripts insertion
		db.EXPECT().
			InsertWorkspaceAgentScripts(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceAgentScript{}, nil)

		// Mock resource metadata insertion
		db.EXPECT().
			InsertWorkspaceResourceMetadata(gomock.Any(), gomock.Any()).
			Return([]database.WorkspaceResourceMetadatum{}, nil)

		// Mock metadata insertions with expected intervals
		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// First call: verify "below" is upgraded to 30s
				require.Equal(t, int64(30), params.Interval, "Expected 'below' interval to be upgraded to 30s")
				require.Equal(t, "below", params.Key)
				return nil
			})

		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// Second call: verify "at" stays at 30s
				require.Equal(t, int64(30), params.Interval, "Expected 'at' interval to stay at 30s")
				require.Equal(t, "at", params.Key)
				return nil
			})

		db.EXPECT().
			InsertWorkspaceAgentMetadata(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, params database.InsertWorkspaceAgentMetadataParams) error {
				// Third call: verify "above" stays at 120s
				require.Equal(t, int64(120), params.Interval, "Expected 'above' interval to stay at 120s")
				require.Equal(t, "above", params.Key)
				return nil
			})

		// Create resource with mixed intervals
		resource := &sdkproto.Resource{
			Name: "example",
			Type: "aws_instance",
			Agents: []*sdkproto.Agent{
				{
					Name: "main",
					Auth: &sdkproto.Agent_Token{},
					Metadata: []*sdkproto.Agent_Metadata{
						{
							Key:         "below",
							DisplayName: "Below Minimum",
							Script:      "echo below",
							Interval:    10, // Below minimum
							Timeout:     5,
						},
						{
							Key:         "at",
							DisplayName: "At Minimum",
							Script:      "echo at",
							Interval:    30, // At minimum
							Timeout:     5,
						},
						{
							Key:         "above",
							DisplayName: "Above Minimum",
							Script:      "echo above",
							Interval:    120, // Above minimum
							Timeout:     5,
						},
					},
				},
			},
		}

		// Should succeed and preserve/upgrade as expected
		err := InsertWorkspaceResource(
			ctx,
			db,
			jobID,
			database.WorkspaceTransitionStart,
			resource,
			&telemetry.Snapshot{},
			InsertWorkspaceResourceWithValidationMode(ValidationModeUpgrade),
			InsertWorkspaceResourceWithDeploymentValues(deploymentValues),
			InsertWorkspaceResourceWithLogger(logger),
		)

		require.NoError(t, err)
	})
}
