package pricing

import (
	"context"
	"fmt"
	"strings"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/ollama"
	"go.uber.org/zap"
)

// RegisterLocalModels registers all local Ollama models with their pricing
func RegisterLocalModels(ctx context.Context, ollamaClient *ollama.Client, pricingClient *Client, serviceType string) error {
	logger.Info("Starting initial model registration")

	// Get list of local models
	models, err := ollamaClient.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list local models: %w", err)
	}

	if len(models.Models) == 0 {
		logger.Info("No local models found to register")
		return nil
	}

	// Extract model names
	modelNames := make([]string, 0, len(models.Models))
	for _, model := range models.Models {
		// Extract base model name (remove tags)
		baseName := strings.Split(model.ID, ":")[0]
		modelNames = append(modelNames, baseName)
	}

	// Get pricing for all models
	prices, err := pricingClient.GetModelPrices(ctx, modelNames)
	if err != nil {
		return fmt.Errorf("failed to get model prices: %w", err)
	}

	// Create a map of model prices for easy lookup
	priceMap := make(map[string]ModelPrice)
	for _, price := range prices.ModelPrices {
		priceMap[price.ModelName] = price
	}

	// Register each model
	for _, modelName := range modelNames {
		price, exists := priceMap[modelName]
		if !exists {
			logger.Warn("No pricing found for model, using default pricing",
				zap.String("model", modelName))
			// Use default pricing
			if err := pricingClient.RegisterModel(ctx, modelName, serviceType, 0.0002, 0.0003); err != nil {
				logger.Error("Failed to register model with default pricing",
					zap.String("model", modelName),
					zap.Error(err))
				continue
			}
		} else {
			if err := pricingClient.RegisterModel(ctx, modelName, serviceType, price.AvgInputPrice, price.AvgOutputPrice); err != nil {
				logger.Error("Failed to register model",
					zap.String("model", modelName),
					zap.Error(err))
				continue
			}
		}
		logger.Info("Successfully registered model",
			zap.String("model", modelName))
	}

	logger.Info("Completed initial model registration",
		zap.Int("total_models", len(modelNames)))
	return nil
}
