package polaris

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-lynx/lynx-polaris/conf"
)

// ValidationError configuration validation error
type ValidationError struct {
	Field   string
	Message string
	Value   interface{}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s (value: %v)", e.Field, e.Message, e.Value)
}

// ValidationResult validation result
type ValidationResult struct {
	IsValid bool
	Errors  []*ValidationError
}

// NewValidationResult creates validation result
func NewValidationResult() *ValidationResult {
	return &ValidationResult{
		IsValid: true,
		Errors:  make([]*ValidationError, 0),
	}
}

// AddError adds error
func (r *ValidationResult) AddError(field, message string, value interface{}) {
	r.IsValid = false
	r.Errors = append(r.Errors, &ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	})
}

// Error returns error message
func (r *ValidationResult) Error() string {
	if r.IsValid {
		return ""
	}

	var messages []string
	for _, err := range r.Errors {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

// Validator configuration validator
type Validator struct {
	config *conf.Polaris
}

// NewValidator creates new validator
func NewValidator(config *conf.Polaris) *Validator {
	return &Validator{
		config: config,
	}
}

// Validate validates configuration
func (v *Validator) Validate() *ValidationResult {
	result := NewValidationResult()
	if v.config == nil {
		result.AddError("config", "configuration is required", nil)
		return result
	}

	// Validate basic fields
	v.validateBasicFields(result)

	// Validate numeric ranges
	v.validateNumericRanges(result)

	// Validate enum values
	v.validateEnumValues(result)

	// Validate time-related configurations
	v.validateTimeConfigs(result)

	// Validate dependencies
	v.validateDependencies(result)

	// Additional: validate security-related configurations
	v.validateSecurityConfigs(result)

	// Additional: validate network-related configurations
	v.validateNetworkConfigs(result)

	// Additional: validate performance-related configurations
	v.validatePerformanceConfigs(result)

	return result
}

// validateBasicFields validates basic fields
func (v *Validator) validateBasicFields(result *ValidationResult) {
	// Validate namespace
	if v.config.Namespace == "" {
		result.AddError("namespace", "namespace cannot be empty", v.config.Namespace)
	} else if len(v.config.Namespace) > 64 {
		result.AddError("namespace", "namespace length must not exceed 64 characters", v.config.Namespace)
	} else {
		// Validate namespace format (only letters, numbers, underscores, and hyphens allowed)
		namespaceRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
		if !namespaceRegex.MatchString(v.config.Namespace) {
			result.AddError("namespace", "namespace can only contain letters, numbers, underscores, and hyphens", v.config.Namespace)
		}
	}

	// Validate Token (if provided). Never put token value in result (security).
	if v.config.Token != "" && len(v.config.Token) > 1024 {
		result.AddError("token", "token length must not exceed 1024 characters", "[REDACTED]")
	}
	if v.config.Token != "" && len(v.config.Token) < 8 {
		result.AddError("token", "token must be at least 8 characters long", "[REDACTED]")
	}
}

// validateNumericRanges validates numeric ranges (single source of truth from conf constants)
func (v *Validator) validateNumericRanges(result *ValidationResult) {
	// Validate weight
	if v.config.Weight < conf.MinWeight || v.config.Weight > conf.MaxWeight {
		result.AddError("weight", fmt.Sprintf("weight must be between %d and %d", conf.MinWeight, conf.MaxWeight), v.config.Weight)
	}

	// Validate TTL (MinTTL=5, MaxTTL=300)
	if v.config.Ttl < conf.MinTTL || v.config.Ttl > conf.MaxTTL {
		result.AddError("ttl", fmt.Sprintf("ttl must be between %d and %d seconds", conf.MinTTL, conf.MaxTTL), v.config.Ttl)
	}

	// Validate retry configuration
	if v.config.MaxRetryTimes < conf.MinRetryTimes || v.config.MaxRetryTimes > conf.MaxRetryTimes {
		result.AddError("max_retry_times", fmt.Sprintf("max_retry_times must be between %d and %d", conf.MinRetryTimes, conf.MaxRetryTimes), v.config.MaxRetryTimes)
	}
}

// validateEnumValues validates enum values
func (v *Validator) validateEnumValues(result *ValidationResult) {
	// No enum value fields in current configuration, skip validation
}

// validateTimeConfigs validates time-related configurations (single source of truth from conf constants)
func (v *Validator) validateTimeConfigs(result *ValidationResult) {
	// Validate timeout (MinTimeoutSeconds=1, MaxTimeoutSeconds=60), use AsDuration for sub-second precision
	if v.config.Timeout != nil {
		timeout := v.config.Timeout.AsDuration()
		minTimeout := time.Duration(conf.MinTimeoutSeconds) * time.Second
		maxTimeout := time.Duration(conf.MaxTimeoutSeconds) * time.Second
		if timeout < minTimeout || timeout > maxTimeout {
			result.AddError("timeout", fmt.Sprintf("timeout must be between %d and %d seconds", conf.MinTimeoutSeconds, conf.MaxTimeoutSeconds), timeout)
		}
	}
}

// validateDependencies validates cross-field dependencies
func (v *Validator) validateDependencies(result *ValidationResult) {
	// Validate coordination between timeout and TTL (timeout should be less than TTL for heartbeat/registration)
	if v.config.Timeout != nil && v.config.Ttl > 0 {
		timeout := v.config.Timeout.AsDuration()
		ttlDuration := time.Duration(v.config.Ttl) * time.Second
		if timeout >= ttlDuration {
			result.AddError("timeout", "timeout should be less than TTL for proper service registration", timeout)
		}
	}
}

// validateSecurityConfigs validates security-related configurations
func (v *Validator) validateSecurityConfigs(result *ValidationResult) {
	// Token complexity check: optional, disabled by default for Polaris compatibility.
	// Enable via POLARIS_ENABLE_TOKEN_COMPLEXITY_CHECK=1 to require letters+digits.
	if v.config.Token != "" && os.Getenv("POLARIS_ENABLE_TOKEN_COMPLEXITY_CHECK") == "1" {
		hasLetter := false
		hasDigit := false
		for _, char := range v.config.Token {
			if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
				hasLetter = true
			}
			if char >= '0' && char <= '9' {
				hasDigit = true
			}
		}
		if !hasLetter || !hasDigit {
			result.AddError("token", "token must contain both letters and numbers (POLARIS_ENABLE_TOKEN_COMPLEXITY_CHECK=1)", "[REDACTED]")
		}
	}

	// Validate namespace security (optional; disable via POLARIS_DISABLE_NAMESPACE_SENSITIVE_CHECK=1 or override list via POLARIS_NAMESPACE_SENSITIVE_WORDS)
	if v.config.Namespace == "" {
		return
	}
	if os.Getenv("POLARIS_DISABLE_NAMESPACE_SENSITIVE_CHECK") == "1" || os.Getenv("POLARIS_DISABLE_NAMESPACE_SENSITIVE_CHECK") == "true" {
		return
	}
	sensitiveChars := defaultNamespaceSensitiveWords()
	if override := os.Getenv("POLARIS_NAMESPACE_SENSITIVE_WORDS"); override != "" {
		sensitiveChars = strings.Split(override, ",")
		for i, s := range sensitiveChars {
			sensitiveChars[i] = strings.TrimSpace(strings.ToLower(s))
		}
	}
	namespaceLower := strings.ToLower(v.config.Namespace)
	for _, sensitive := range sensitiveChars {
		if sensitive == "" {
			continue
		}
		if strings.Contains(namespaceLower, sensitive) {
			result.AddError("namespace", fmt.Sprintf("namespace should not contain sensitive word: %s", sensitive), v.config.Namespace)
			return
		}
	}
}

// defaultNamespaceSensitiveWords returns the default list when not overridden by env
func defaultNamespaceSensitiveWords() []string {
	return []string{"admin", "root", "system", "internal"}
}

// validateNetworkConfigs validates network-related configurations (retry covered by validateNumericRanges)
func (v *Validator) validateNetworkConfigs(result *ValidationResult) {
	// No additional network validations; timeout and retry use conf constants
}

// validatePerformanceConfigs validates performance-related configurations
// Weight and TTL are already validated in validateNumericRanges using conf constants
func (v *Validator) validatePerformanceConfigs(result *ValidationResult) {
	// No additional validations; weight and TTL use conf.MinWeight/MaxWeight, conf.MinTTL/MaxTTL
}

// ValidateConfig convenient configuration validation function
func ValidateConfig(config *conf.Polaris) error {
	validator := NewValidator(config)
	result := validator.Validate()

	if !result.IsValid {
		return fmt.Errorf("configuration validation failed: %s", result.Error())
	}

	return nil
}
