High Priority - Missing Tests                                                                                                        
                                                                                                                                       
  1. Public Method Tests                                                                                                               
                                                                                                                                       
  - New() - creation with nil/custom config, default registry behavior                                                                 
  - WithEvents() - registry replacement, chaining, event routing                                                                       
  - Subscribe() - subscriber addition, chaining, multiple subscribers                                                                  
                                                                                                                                       
  2. Execute Lifecycle Events                                                                                                          
                                                                                                                                       
  - BeforeExecutionEvent published before first iteration                                                                              
  - AfterExecutionEvent published exactly once after termination (including on error)                                                  
  - AfterExecutionEvent contains correct TerminationReason and Error                                                                   
  - Event publishing order: BeforeExecution → BeforeIteration → AfterIteration → ... → AfterExecution                                  
                                                                                                                                       
  3. Successful Termination (LATerminate)                                                                                              
                                                                                                                                       
  - Execution terminates when AgentLoop returns LATerminate                                                                            
  - Result stored in ExecutionContext                                                                                                  
  - TerminationSuccess is set, Error is nil                                                                                            
                                                                                                                                       
  4. Error Handling                                                                                                                    
                                                                                                                                       
  - Loop.Next() errors are wrapped with iteration number                                                                               
  - Error message format is correct                                                                                                    
  - TerminationError is set                                                                                                            
  - AfterIterationEvent published before returning on error                                                                            
                                                                                                                                       
  5. Stream Lifecycle                                                                                                                  
                                                                                                                                       
  - Streams opened when execution starts                                                                                               
  - CloseStreams() called exactly once in defer (even on panic/error)                                                                  
                                                                                                                                       
  6. Duration Tracking                                                                                                                 
                                                                                                                                       
  - AfterIterationEvent.Duration >= actual elapsed time                                                                                
  - Individual durations per iteration                                                                                                 
                                                                                                                                       
  Medium Priority - Missing Tests                                                                                                      
                                                                                                                                       
  7. Context Cancellation                                                                                                              
                                                                                                                                       
  - User cancellation terminates execution                                                                                             
  - TerminationContextCanceled is set                                                                                                  
  - Cancel during first/Nth iteration behavior                                                                                         
                                                                                                                                       
  8. Limit-Event Interactions                                                                                                          
                                                                                                                                       
  - All iteration events published before limit error returned                                                                         
  - Error vs limit priority (Loop error takes precedence)                                                                              
  - Limit error message format: "limit exceeded: {Key} > {MaxValue}"                                                                   
                                                                                                                                       
  9. Stats Reset Behavior                                                                                                              
                                                                                                                                       
  - Consecutive error counter resets to 0 on success                                                                                   
  - Resets applied before next iteration                                                                                               
                                                                                                                                       
  10. Result Validation                                                                                                                
                                                                                                                                       
  - ExecutionResult.Output matches AgentLoopResult.Result                                                                              
  - Iteration count in result matches actual iterations                                                                                
                                                                                                                                       
  11. Subscriber Behavior                                                                                                              
                                                                                                                                       
  - BeforeExecutionSubscriber called before first iteration                                                                            
  - AfterExecutionSubscriber called after termination                                                                                  
  - AfterIterationSubscriber called after each iteration                                                                               
  - Multiple subscribers all receive events                                                                                            
                                                                                                                                       
  12. Concurrency Safety                                                                                                               
                                                                                                                                       
  - Race condition tests with -race flag                                                                                               
  - Concurrent stats updates from children                                                                                             
  - Event ordering under concurrency                                                                                                   
                                                                                                                                       
  ---                                                                                                                                  
  Summary: Current tests (47 functions, ~1529 lines) focus heavily on limit enforcement. Missing ~74+ tests covering the execute       
  lifecycle, event publishing, error handling, and public API behaviors. 