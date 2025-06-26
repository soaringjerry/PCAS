# PCAS Examples

This directory contains example D-Apps and usage patterns for PCAS.

## Available Examples

### 1. Hello D-App
**Location**: `hello-dapp/`

A simple D-App that demonstrates:
- Connecting to PCAS
- Subscribing to events
- Processing and displaying events

Perfect for learning the basics of D-App development.

## Running Examples

All examples follow the same pattern:

1. Start PCAS (from the root directory):
   ```bash
   make dev-up
   ```

2. Navigate to the example directory:
   ```bash
   cd examples/hello-dapp
   ```

3. Run the example:
   ```bash
   go run .
   ```

## Contributing Examples

When adding new examples:
- Keep them simple and focused on demonstrating specific features
- Include a README.md with clear instructions
- Add appropriate comments in the code
- Ensure they work with the current version of PCAS SDK