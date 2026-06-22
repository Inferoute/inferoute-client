package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
	"go.uber.org/zap"
)

// RegisterLocalModels registers verified local models with their pricing.
func RegisterLocalModels(ctx context.Context, llmClient llm.Client, pricingClient *Client, serviceType string, verifier *verify.Verifier, verificationEnabled bool) ([]string, error) {
	// Normalize service type to match API expectations
	normalizedServiceType := strings.ToLower(serviceType)
	if normalizedServiceType != "vllm" && normalizedServiceType != "ollama" {
		logger.Warn("Invalid service type, defaulting to vllm",
			zap.String("original_service_type", serviceType),
			zap.String("normalized_service_type", "vllm"))
		normalizedServiceType = "vllm"
	}

	logger.Info("Starting initial model registration",
		zap.String("original_service_type", serviceType),
		zap.String("normalized_service_type", normalizedServiceType))

	// Get list of local models
	models, err := llmClient.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list local models: %w", err)
	}

	if len(models.Models) == 0 {
		logger.Info("No local models found to register")
		return nil, nil
	}

	modelList := models.Models
	if verificationEnabled && verifier != nil {
		modelList = verifier.ApplyToModels(ctx, llmClient, models.Models)
	}

	// Extract model names (verified only when verification is enabled)
	modelNames := make([]string, 0, len(modelList))
	for _, model := range modelList {
		if verificationEnabled && !verify.IsInferenceAllowed(model.VerificationStatus) {
			logger.Warn("Skipping unverified model at registration",
				zap.String("model", model.ID),
				zap.String("verification_status", model.VerificationStatus))
			continue
		}
		modelNames = append(modelNames, model.ID)
	}

	if len(modelNames) == 0 {
		logger.Info("No verified models to register")
		return nil, nil
	}

	// Get pricing for all models
	prices, err := pricingClient.GetModelPrices(ctx, modelNames)
	if err != nil {
		return nil, fmt.Errorf("failed to get model prices: %w", err)
	}

	logger.Info("Received pricing information from API",
		zap.Any("prices", prices.ModelPrices))

	// Create a map of model prices for easy lookup and find default pricing
	priceMap := make(map[string]ModelPrice)
	var defaultPrice ModelPrice
	for _, price := range prices.ModelPrices {
		if price.ModelName == "default" {
			defaultPrice = price
			logger.Info("Found default pricing",
				zap.Float64("default_input_price", defaultPrice.AvgInputPrice),
				zap.Float64("default_output_price", defaultPrice.AvgOutputPrice))
			continue
		}
		priceMap[price.ModelName] = price
		logger.Debug("Mapped price for model",
			zap.String("model", price.ModelName),
			zap.Float64("input_price", price.AvgInputPrice),
			zap.Float64("output_price", price.AvgOutputPrice),
			zap.Int("sample_size", price.SampleSize))
	}

	if defaultPrice.ModelName == "" {
		logger.Warn("No default pricing found in API response, using hardcoded defaults")
		defaultPrice = ModelPrice{
			ModelName:      "default",
			AvgInputPrice:  0.0002,
			AvgOutputPrice: 0.0003,
		}
	}

	// Register each model
	for _, modelName := range modelNames {
		price, exists := priceMap[modelName]
		if !exists {
			logger.Info("No specific pricing found for model, using default pricing",
				zap.String("model", modelName),
				zap.Float64("default_input_price", defaultPrice.AvgInputPrice),
				zap.Float64("default_output_price", defaultPrice.AvgOutputPrice))

			logger.Debug("Registering model with service type",
				zap.String("model", modelName),
				zap.String("service_type", normalizedServiceType))

			if err := pricingClient.RegisterModel(ctx, modelName, normalizedServiceType, defaultPrice.AvgInputPrice, defaultPrice.AvgOutputPrice); err != nil {
				if errors.Is(err, ErrModelAlreadyExists) {
					logger.Info("Model already registered, skipping",
						zap.String("model", modelName),
						zap.String("service_type", normalizedServiceType))
				} else {
					logger.Error("Failed to register model with default pricing",
						zap.String("model", modelName),
						zap.String("service_type", normalizedServiceType),
						zap.Error(err))
				}
				continue
			}
		} else {
			logger.Info("Registering model with specific pricing",
				zap.String("model", modelName),
				zap.Float64("input_price", price.AvgInputPrice),
				zap.Float64("output_price", price.AvgOutputPrice),
				zap.Int("sample_size", price.SampleSize))

			logger.Debug("Registering model with service type",
				zap.String("model", modelName),
				zap.String("service_type", normalizedServiceType))

			if err := pricingClient.RegisterModel(ctx, modelName, normalizedServiceType, price.AvgInputPrice, price.AvgOutputPrice); err != nil {
				if errors.Is(err, ErrModelAlreadyExists) {
					logger.Info("Model already registered, skipping",
						zap.String("model", modelName),
						zap.String("service_type", normalizedServiceType))
				} else {
					logger.Error("Failed to register model",
						zap.String("model", modelName),
						zap.String("service_type", normalizedServiceType),
						zap.Error(err))
				}
				continue
			}
		}
		logger.Info("Successfully registered model",
			zap.String("model", modelName))
	}

	logger.Info("Completed initial model registration",
		zap.Int("total_models", len(modelNames)))
	return modelNames, nil
}
