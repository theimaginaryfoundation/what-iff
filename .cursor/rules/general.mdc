---
description: 
globs: *
alwaysApply: false
---
# General Project Guidelines

## Project Overview
This project is a personal assistant AI agent. It's designed to conduct research, help write emails, documents, and other content. It's also designed to be extensible via MCP to support integrations with external services to enable additional agent capabilities. 

## Project Structure
The project follows standard Go project layout conventions:
- `cmd/` - Main applications for this project
  - `api-server/` - Backend API server for the personal assistant
- `internal/` - Library code that can be used by other applications
  - `datastore/` - Database access layer using Ent ORM
  - `models/` - Data structures shared across the application
  - `server/` - Main startup logic for API server
  - `handlers/` - API endpoint handlers for API server
  - `database/` - Database connection and configuration utilities
- `ent/` - Auto-generated Ent ORM code for database entities

## Development Best Practices

### Go Coding Standards
1. **Idiomatic Go**: Follow the Go proverbs and standard conventions
   - Use proper error handling (no panics in production code)
   - Embrace interfaces for dependency inversion
   - Prefer composition over inheritance
   - Use meaningful variable names (`i` for indexes, `err` for errors)

2. **Code Organization**:
   - Keep functions small and focused on a single responsibility
   - Group related functions in packages with clear boundaries
   - Use interfaces to define behavior at package boundaries
   - Extract complex logic into well-named helper functions immediately
   - Avoid deeply nested loops and conditionals (aim for cyclomatic complexity < 10)
   - Use early returns to reduce nesting levels

3. **Error Handling**:
   - Always check errors, never use `_` to ignore errors without justification
   - Return errors rather than using panic
   - Use custom error types or error wrapping for context
   - Use `fmt.Errorf()` with `%w` for wrapping errors

4. **Concurrency**:
   - Use channels and goroutines appropriately
   - Always ensure proper goroutine termination
   - Use context for cancellation and timeouts
   - Use sync primitives (Mutex, WaitGroup) when appropriate

5. **Function Design and Complexity Management**:
   - Write small, focused functions from the start (avoid large monolithic functions)
   - When a function grows beyond ~50 lines, consider extracting helper functions
   - Use helper functions to make code self-documenting through clear naming
   - Avoid nesting more than 3 levels deep - extract nested logic into separate functions
   - Use early returns to reduce nesting and improve readability
   - Replace complex `if-else` chains with `switch` statements or lookup tables
   
   ```go
   // Good: Clear, focused functions with minimal nesting
   func handleRequest(req Request) error {
       if !req.Valid {
           return ErrInvalidRequest
       }
       
       for _, item := range req.Items {
           if err := processItem(item); err != nil {
               return err
           }
       }
       return nil
   }
   
   func processItem(item Item) error {
       switch item.Type {
       case "A":
           return processTypeA(item)
       case "B":
           return processTypeB(item)
       default:
           return ErrUnknownType
       }
   }
   ```

6. **Data Structure Design for Maintainability**:
   - Design structures that are self-contained and capture all needed data at creation
   - Avoid parallel arrays or slices that must stay synchronized by index
   - When processing data, capture all context needed for downstream operations
   - Prefer structures that eliminate the need for data merging or complex lookups
   
   ```go
   // Good: Self-contained structure with all needed data
   type ProcessingResult struct {
       ID        string  // Captured at creation
       ToolName  string  // Captured at creation
       ToolInput string  // Captured at creation
       Output    string
       Error     error
   }
   
   func executeAndCapture(toolCall ToolCall) ProcessingResult {
       var result string
       var err error
       // ... execution logic ...
       
       return ProcessingResult{
           ID:        toolCall.ID,
           ToolName:  toolCall.Name,
           ToolInput: toolCall.Arguments,
           Output:    result,
           Error:     err,
       }
   }
   
   // No merging needed - all data is in one place
   func convertToModels(results []ProcessingResult) []*Model {
       models := make([]*Model, len(results))
       for i, result := range results {
           models[i] = &Model{
               Name:   result.ToolName,
               Input:  result.ToolInput,
               Output: result.Output,
               Error:  result.Error,
           }
       }
       return models
   }
   ```

7. **Designing for Testability**:
   - Avoid tight coupling to third-party SDK types that are difficult to construct in tests
   - Create intermediate data structures that capture only the data you need
   - Design pure functions that take simple inputs and produce outputs without side effects
   - Separate business logic from SDK/API interaction code
   - Make data structures easy to construct with simple field assignments
   
   ```go
   // Good: Business logic separated from SDK types
   type ToolCallData struct {
       ID        string
       Name      string
       Arguments string
   }
   
   // Easy to test - takes simple struct, returns simple struct
   func validateToolCall(data ToolCallData) error {
       if data.Name == "" {
           return ErrEmptyName
       }
       if data.Arguments == "" {
           return ErrEmptyArguments
       }
       return nil
   }
   
   // SDK interaction isolated in adapter function
   func executeToolCall(sdkToolCall sdk.ToolCall) (ToolCallData, error) {
       data := ToolCallData{
           ID:        sdkToolCall.ID,
           Name:      sdkToolCall.Name,
           Arguments: sdkToolCall.Arguments,
       }
       
       if err := validateToolCall(data); err != nil {
           return ToolCallData{}, err
       }
       
       return data, nil
   }
   ```

8. **Explicit Error Handling and Nil Safety**:
   - Always return errors explicitly - never return `nil` silently on failure
   - Add `nil` checks immediately after function calls that can return `nil` pointers
   - Use `(value, error)` return pattern for functions that produce values
   - Validate that expected non-nil values are actually non-nil before using them
   
   ```go
   // Good: Explicit error returns and nil checks
   func getResponse(ctx context.Context) (*Response, error) {
       resp, err := callAPI(ctx)
       if err != nil {
           return nil, fmt.Errorf("failed to call API: %w", err)
       }
       
       if resp == nil {
           return nil, fmt.Errorf("API returned nil response")
       }
       
       return resp, nil
   }
   
   // Caller handles errors and checks for nil
   func processRequest(ctx context.Context) error {
       resp, err := getResponse(ctx)
       if err != nil {
           return fmt.Errorf("failed to get response: %w", err)
       }
       
       if resp == nil {
           return fmt.Errorf("unexpected nil response")
       }
       
       return processResponse(resp)
   }
   ```

### Documentation
1. **Go Doc Comments**:
   - Every exported function, type, and package must have a doc comment
   - Begin with the name of the element being documented
   - Use complete sentences with proper punctuation
   - Document parameters and return values
   ```go
   // UserService provides methods to manage users in the system.
   // It handles authentication, authorization, and user profile management.
   type UserService struct {
       // ...
   }
   
   // CreateUser creates a new user in the system with the provided details.
   // It validates the input and returns an error if validation fails.
   // Returns the created user's ID on success.
   func (s *UserService) CreateUser(ctx context.Context, user *models.User) (int64, error) {
       // ...
   }
   ```

2. **README Files**:
   - Each major component should have a README.md explaining its purpose
   - Include setup instructions, usage examples, and design decisions

### Testing

1. **Unit Testing Principles**:
   - Write tests for all new code
   - Use table-driven tests for functions with multiple cases
   - Keep tests independent of each other
   - Tests should be fast and deterministic

2. **Test Structure**:
   - Use descriptive test names (`TestCreateUser_ValidInput_Success`)
   - Follow Arrange-Act-Assert (AAA) pattern
   - Use subtests for multiple test cases
   ```go
   func TestCreateUser(t *testing.T) {
       tests := []struct {
           name        string
           input       models.User
           expectedID  int64
           expectError bool
       }{
           // test cases here
       }
       
       for _, tc := range tests {
           t.Run(tc.name, func(t *testing.T) {
               // Arrange
               svc := NewUserService(mockRepo)
               
               // Act
               id, err := svc.CreateUser(context.Background(), &tc.input)
               
               // Assert
               if tc.expectError {
                   require.Error(t, err)
               } else {
                   require.NoError(t, err)
                   require.Equal(t, tc.expectedID, id)
               }
           })
       }
   }
   ```

3. **Dependency Inversion**:
   - Use interfaces for dependencies to enable mocking
   - Inject dependencies rather than creating them inside functions
   - Write code to be testable from the beginning

4. **Mocking**:
   - Use interfaces and dependency injection to enable mocking
   - Consider gomock or testify/mock for generating mocks
   - Keep mocks simple and focused on the behavior needed for tests

5. **Integration Testing**:
   - Write integration tests for critical paths
   - Use test containers or embedded databases for integration tests
   - Clearly separate unit tests from integration tests

### Development Workflow

1. **Incremental Changes**:
   - Work in small, focused changes
   - Commit frequently with descriptive commit messages
   - Run tests after each significant change
   - Avoid large PRs that are difficult to review

2. **Test-Driven Development**:
   - Write tests before implementation when appropriate
   - Follow Red-Green-Refactor cycle:
     - Red: Write a failing test
     - Green: Implement the minimum code to make the test pass
     - Refactor: Clean up the code while keeping tests green

3. **Documentation Updates**:
   - Update README files whenever functionality changes
   - Keep documentation in sync with code changes
   - Document new features, configuration options, and API changes
   - Update examples to reflect current usage patterns
   - Treat documentation updates as part of the definition of "done" for any task

4. **Code Review**:
   - Review your own code before submitting for review
   - Keep PRs small and focused on a single concern
   - Respond to feedback constructively
   - Look for edge cases and error handling during review

### Scope Management

1. **Stay Focused**:
   - Implement only what is explicitly requested
   - Do not extend scope by adding "nice-to-have" features without discussion
   - Do not refactor unrelated code while implementing a feature

2. **Technical Debt Management**:
   - Document technical debt with TODO comments (include ticket numbers if applicable)
   - Address technical debt in dedicated refactoring tasks
   - Prioritize critical technical debt that affects reliability or security

### Error and Test Failure Handling

1. **Test Failures**:
   - Never remove tests just to make them pass
   - Never add code that detects test scenarios to behave differently
   - Address the root cause of test failures
   - If a test is outdated, update the test to reflect new requirements

2. **Production Error Handling**:
   - Log errors with appropriate context
   - Return meaningful error messages
   - Use structured logging with fields for context
   - Don't expose internal errors to users

### Performance and Security

1. **Performance Considerations**:
   - Write efficient code but prioritize readability
   - Profile before optimizing
   - Document performance requirements and considerations
   - Use benchmarks to validate performance improvements

2. **Security Best Practices**:
   - Validate all user input
   - Use prepared statements for database queries
   - Follow the principle of least privilege
   - Keep dependencies updated and scan for vulnerabilities

## Continuous Integration

1. **CI Practices**:
   - All code must pass tests before merging
   - Run linters and static analysis tools in CI
   - Maintain test coverage metrics
