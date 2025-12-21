package jobs

import (
	"context"
	"fmt"
	"log"
	"time"
)

// Example job handlers for common use cases

// EmailJobHandler handles sending email notifications
func EmailJobHandler(ctx context.Context, job *Job) error {
	recipient, err := job.GetPayloadString("recipient")
	if err != nil {
		return fmt.Errorf("missing recipient: %v", err)
	}
	
	subject, err := job.GetPayloadString("subject")
	if err != nil {
		return fmt.Errorf("missing subject: %v", err)
	}
	
	body, err := job.GetPayloadString("body")
	if err != nil {
		return fmt.Errorf("missing body: %v", err)
	}
	
	// Simulate email sending
	log.Printf("Sending email to %s with subject: %s (body length: %d)", recipient, subject, len(body))
	time.Sleep(500 * time.Millisecond) // Simulate network delay
	log.Printf("Email sent successfully to %s", recipient)
	
	// Return result that can be stored
	job.Result = map[string]interface{}{
		"status":    "sent",
		"timestamp": time.Now(),
		"recipient": recipient,
	}
	
	return nil
}

// ImageProcessingJobHandler handles image processing tasks
func ImageProcessingJobHandler(ctx context.Context, job *Job) error {
	imagePath, err := job.GetPayloadString("image_path")
	if err != nil {
		return fmt.Errorf("missing image_path: %v", err)
	}
	
	operation, err := job.GetPayloadString("operation")
	if err != nil {
		return fmt.Errorf("missing operation: %v", err)
	}
	
	log.Printf("Processing image %s with operation: %s", imagePath, operation)
	
	// Simulate image processing time based on operation
	switch operation {
	case "thumbnail":
		time.Sleep(1 * time.Second)
	case "resize":
		time.Sleep(2 * time.Second)
	case "watermark":
		time.Sleep(3 * time.Second)
	default:
		return fmt.Errorf("unknown operation: %s", operation)
	}
	
	outputPath := fmt.Sprintf("%s_%s_processed.jpg", imagePath, operation)
	log.Printf("Image processed successfully, saved to: %s", outputPath)
	
	job.Result = map[string]interface{}{
		"status":      "processed",
		"input_path":  imagePath,
		"output_path": outputPath,
		"operation":   operation,
	}
	
	return nil
}

// DataExportJobHandler handles large data exports
func DataExportJobHandler(ctx context.Context, job *Job) error {
	format, err := job.GetPayloadString("format")
	if err != nil {
		return fmt.Errorf("missing format: %v", err)
	}
	
	query, err := job.GetPayloadString("query")
	if err != nil {
		return fmt.Errorf("missing query: %v", err)
	}
	
	recordCount, err := job.GetPayloadInt("record_count")
	if err != nil {
		recordCount = 1000 // default
	}
	
	log.Printf("Starting data export: %d records in %s format using query: %s", recordCount, format, query)
	
	// Simulate export progress
	total := recordCount
	for i := 0; i < total; i += 100 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(100 * time.Millisecond)
			progress := (i + 100) * 100 / total
			if progress > 100 {
				progress = 100
			}
			log.Printf("Export progress: %d%%", progress)
		}
	}
	
	exportFile := fmt.Sprintf("export_%s_%d.%s", time.Now().Format("20060102_150405"), recordCount, format)
	log.Printf("Data export completed: %s", exportFile)
	
	job.Result = map[string]interface{}{
		"status":       "completed",
		"file":         exportFile,
		"record_count": recordCount,
		"format":       format,
	}
	
	return nil
}

// NotificationCleanupJobHandler handles cleanup of old notifications
func NotificationCleanupJobHandler(ctx context.Context, job *Job) error {
	daysOld, err := job.GetPayloadInt("days_old")
	if err != nil {
		daysOld = 30 // default to 30 days
	}
	
	log.Printf("Starting cleanup of notifications older than %d days", daysOld)
	
	// Simulate cleanup process
	cleaned := 0
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(200 * time.Millisecond)
			cleaned += 15 + (i * 3) // Simulate variable cleanup counts
		}
	}
	
	log.Printf("Cleanup completed: removed %d old notifications", cleaned)
	
	job.Result = map[string]interface{}{
		"status":           "completed",
		"notifications_removed": cleaned,
		"days_old":         daysOld,
	}
	
	return nil
}

// Example of how to set up background jobs in your Tjo application
func ExampleSetup(manager *JobManager) {
	// Register job handlers
	manager.RegisterHandlerFunc("send_email", EmailJobHandler)
	manager.RegisterHandlerFunc("process_image", ImageProcessingJobHandler)
	manager.RegisterHandlerFunc("export_data", DataExportJobHandler)
	manager.RegisterHandlerFunc("cleanup_notifications", NotificationCleanupJobHandler)
	
	// Add event listeners for monitoring
	manager.AddEventListenerFunc(func(event *JobEvent) {
		switch event.Type {
		case EventJobCompleted:
			log.Printf("Job %s (%s) completed successfully", event.Job.ID, event.Job.Type)
		case EventJobFailed:
			log.Printf("Job %s (%s) failed: %v", event.Job.ID, event.Job.Type, event.Error)
		case EventJobRetrying:
			log.Printf("Job %s (%s) retrying (attempt %d)", event.Job.ID, event.Job.Type, event.Job.Attempts)
		}
	})
	
	// Schedule recurring cleanup job every day at midnight
	manager.ScheduleCron("0 0 * * *", "cleanup_notifications", "maintenance", map[string]interface{}{
		"days_old": 30,
	})
	
	log.Println("Background job system initialized with example handlers")
}

// Example of how to enqueue jobs in your handlers
func ExampleUsage(manager *JobManager) {
	// Enqueue an immediate email job
	emailJob := NewJob("send_email", "notifications", map[string]interface{}{
		"recipient": "user@example.com",
		"subject":   "Welcome to our service!",
		"body":      "Thank you for signing up.",
	})
	manager.Enqueue(emailJob)
	
	// Enqueue a high-priority image processing job
	imageJob := NewJob("process_image", "media", map[string]interface{}{
		"image_path": "/uploads/photo.jpg",
		"operation":  "thumbnail",
	})
	imageJob.WithPriority(PriorityHigh)
	manager.Enqueue(imageJob)
	
	// Schedule a delayed data export (1 hour from now)
	exportJob := NewJob("export_data", "exports", map[string]interface{}{
		"format":       "csv",
		"query":        "SELECT * FROM users WHERE active = true",
		"record_count": 5000,
	})
	manager.EnqueueIn(exportJob, time.Hour)
	
	log.Println("Example jobs enqueued successfully")
}

// Example error handling job that shows retry behavior
func ErrorProneJobHandler(ctx context.Context, job *Job) error {
	shouldFail, _ := job.GetPayloadBool("should_fail")
	attemptLimit, err := job.GetPayloadInt("fail_until_attempt")
	if err != nil {
		attemptLimit = 2
	}
	
	if shouldFail && job.Attempts < attemptLimit {
		return fmt.Errorf("simulated failure on attempt %d", job.Attempts)
	}
	
	log.Printf("Job %s succeeded on attempt %d", job.ID, job.Attempts)
	return nil
}

// Example of custom retry configuration
func ExampleCustomRetrySetup() *JobManager {
	config := DefaultManagerConfig()
	config.RetryConfig = RetryConfig{
		BaseDelay:     2 * time.Second,
		MaxDelay:      5 * time.Minute,
		MaxAttempts:   5,
		BackoffFactor: 1.5,
		EnableJitter:  true,
	}
	
	manager := NewJobManager(config)
	manager.RegisterHandlerFunc("error_prone", ErrorProneJobHandler)
	
	return manager
}