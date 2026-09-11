package media

import "testing"

func TestCreatorPipelineDecisions(t *testing.T) {
	tests := []struct {
		name      string
		advance   []Stage
		wantStage Stage
		wantErr   bool
	}{
		{name: "processing job reaches creator delivery", advance: []Stage{StageProcessing, StageReady, StageDelivered}, wantStage: StageDelivered},
		{name: "delivery cannot skip processing", advance: []Stage{StageDelivered}, wantStage: StageIngested, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := NewPipeline()
			asset, err := pipeline.Ingest(Asset{ID: "asset-42", Creator: "creator-9", Title: "Episode 12"})
			if err != nil {
				t.Fatal(err)
			}
			for _, stage := range tt.advance {
				next, advanceErr := pipeline.Advance(asset.ID, stage)
				if advanceErr != nil {
					err = advanceErr
					break
				}
				asset = next
			}
			if (err != nil) != tt.wantErr || asset.Stage != tt.wantStage {
				t.Fatalf("stage/error = %s/%v, want %s/error=%v", asset.Stage, err, tt.wantStage, tt.wantErr)
			}
		})
	}
}
