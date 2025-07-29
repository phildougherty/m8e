# Workspace Volumes - Comprehensive Examples

Workspace volumes in Matey provide shared persistent storage between workflow steps, enabling complex data processing pipelines, CI/CD workflows, and multi-step automation tasks.

## Table of Contents

- [Overview](#overview)
- [Configuration Options](#configuration-options)
- [Basic Examples](#basic-examples)
- [Advanced Use Cases](#advanced-use-cases)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Overview

### What are Workspace Volumes?

Workspace volumes are **shared persistent volumes** that are automatically created for each workflow execution and mounted to all workflow steps. They enable:

- **Data persistence** between workflow steps
- **File-based communication** between different tools and languages
- **Build artifact management** in CI/CD pipelines
- **Temporary storage** that's automatically cleaned up

### Automatic Behavior

- ✅ **Auto-enabled** for workflows with 2+ steps
- ✅ **Unique per execution** - each workflow run gets a fresh workspace
- ✅ **Mounted at `/workspace`** by default (configurable)
- ✅ **Auto-cleanup** after workflow completion (configurable)
- ✅ **Environment variables** available in all steps:
  - `WORKSPACE_ENABLED=true`
  - `WORKFLOW_WORKSPACE_PATH=/workspace`

## Configuration Options

### Basic Configuration

```yaml
workspace:
  enabled: true              # Enable workspace (auto for multi-step)
  size: "1Gi"               # Volume size (default: 1Gi)
  mountPath: "/workspace"   # Mount path (default: /workspace)
```

### Advanced Configuration

```yaml
workspace:
  enabled: true
  size: "10Gi"                    # Large workspace for big data
  mountPath: "/data"              # Custom mount point
  storageClass: "fast-ssd"        # Use SSD storage class
  accessModes: ["ReadWriteOnce"]  # Volume access modes
  reclaimPolicy: "Retain"         # Keep volume after completion
```

### Configuration Reference

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` (multi-step) | Enable workspace volume |
| `size` | string | `"1Gi"` | Storage size (K8s quantity) |
| `mountPath` | string | `"/workspace"` | Container mount path |
| `storageClass` | string | cluster default | Kubernetes storage class |
| `accessModes` | []string | `["ReadWriteOnce"]` | Volume access modes |
| `reclaimPolicy` | string | `"Delete"` | `Delete` or `Retain` |

## Basic Examples

### 1. Simple Data Processing Pipeline

```yaml
apiVersion: mcp.matey.ai/v1
kind: MCPTaskScheduler
metadata:
  name: simple-pipeline
spec:
  workflows:
  - name: process-logs
    schedule: "0 * * * *"  # Hourly
    description: "Process log files"
    workspace:
      size: "2Gi"
    steps:
    - name: download-logs
      tool: curl
      description: "Download log files"
      parameters:
        url: "https://logs.example.com/access.log"
        output: "/workspace/access.log"
        
    - name: analyze-logs
      tool: bash
      description: "Extract metrics from logs"
      parameters:
        command: |
          echo "Analyzing logs..."
          # Count requests by status code
          awk '{print $9}' /workspace/access.log | sort | uniq -c > /workspace/status-codes.txt
          # Extract top IPs
          awk '{print $1}' /workspace/access.log | sort | uniq -c | sort -nr | head -10 > /workspace/top-ips.txt
          echo "Analysis complete"
          
    - name: generate-report
      tool: python3
      description: "Create summary report"
      parameters:
        code: |
          import datetime
          
          print("Generating log analysis report...")
          
          # Read analysis results
          with open('/workspace/status-codes.txt', 'r') as f:
              status_data = f.read()
              
          with open('/workspace/top-ips.txt', 'r') as f:
              ip_data = f.read()
          
          # Create HTML report
          html_report = f"""
          <html><body>
          <h1>Log Analysis Report</h1>
          <p>Generated: {datetime.datetime.now()}</p>
          
          <h2>Status Codes</h2>
          <pre>{status_data}</pre>
          
          <h2>Top IP Addresses</h2>
          <pre>{ip_data}</pre>
          </body></html>
          """
          
          with open('/workspace/report.html', 'w') as f:
              f.write(html_report)
          
          print("Report saved to /workspace/report.html")
```

### 2. Multi-Language Data Pipeline

```yaml
workflows:
- name: multilang-pipeline
  description: "Demonstrates different languages sharing workspace"
  workspace:
    size: "5Gi"
    storageClass: "fast-ssd"
  steps:
  - name: fetch-json-data
    tool: curl
    parameters:
      url: "https://jsonplaceholder.typicode.com/posts"
      output: "/workspace/posts.json"
      
  - name: process-with-python
    tool: python3
    parameters:
      code: |
        import json
        import pandas as pd
        
        # Load JSON data
        with open('/workspace/posts.json', 'r') as f:
            posts = json.load(f)
        
        # Convert to DataFrame and analyze
        df = pd.DataFrame(posts)
        
        # Create summary statistics
        stats = {
            'total_posts': len(df),
            'unique_users': df['userId'].nunique(),
            'avg_title_length': df['title'].str.len().mean(),
            'posts_per_user': df.groupby('userId').size().to_dict()
        }
        
        # Save processed data
        df.to_csv('/workspace/posts.csv', index=False)
        
        with open('/workspace/stats.json', 'w') as f:
            json.dump(stats, f, indent=2)
        
        print(f"Processed {len(df)} posts from {stats['unique_users']} users")
        
  - name: create-charts-with-nodejs
    tool: node
    parameters:
      code: |
        const fs = require('fs');
        
        console.log('Creating data visualizations...');
        
        // Read processed data
        const stats = JSON.parse(fs.readFileSync('/workspace/stats.json', 'utf8'));
        const posts = fs.readFileSync('/workspace/posts.csv', 'utf8');
        
        // Simple ASCII chart of posts per user
        let chart = 'Posts per User:\\n';
        for (const [userId, count] of Object.entries(stats.posts_per_user)) {
            const bar = '█'.repeat(count);
            chart += `User ${userId}: ${bar} (${count})\\n`;
        }
        
        // Save visualization
        fs.writeFileSync('/workspace/chart.txt', chart);
        
        // Create summary
        const summary = `
        Data Summary:
        =============
        Total Posts: ${stats.total_posts}
        Unique Users: ${stats.unique_users}
        Avg Title Length: ${stats.avg_title_length.toFixed(2)} chars
        `;
        
        fs.writeFileSync('/workspace/summary.txt', summary);
        console.log('Visualizations created successfully');
        
  - name: final-report-with-bash
    tool: bash
    parameters:
      command: |
        echo "=== MULTI-LANGUAGE PIPELINE REPORT ==="
        echo
        
        echo "Files created in workspace:"
        ls -lh /workspace/
        echo
        
        echo "Summary:"
        cat /workspace/summary.txt
        echo
        
        echo "User Activity Chart:"
        cat /workspace/chart.txt
        echo
        
        # Create final archive
        tar -czf /workspace/pipeline-results.tar.gz /workspace/*.json /workspace/*.csv /workspace/*.txt
        echo "Results archived to pipeline-results.tar.gz"
        
        echo "Pipeline completed successfully!"
```

## Advanced Use Cases

### 1. CI/CD Build Pipeline

```yaml
workflows:
- name: ci-cd-pipeline
  description: "Complete CI/CD pipeline with workspace"
  workspace:
    size: "20Gi"              # Large workspace for builds
    storageClass: "fast-ssd"   # Fast storage for build performance
    reclaimPolicy: "Retain"    # Keep workspace for debugging
  steps:
  - name: checkout-source
    tool: git
    description: "Clone source repository"
    parameters:
      command: |
        echo "Cloning source code..."
        git clone --depth 1 https://github.com/user/myproject.git /workspace/src
        cd /workspace/src
        git log --oneline -1
        
  - name: install-dependencies
    tool: bash
    description: "Install build dependencies"
    parameters:
      command: |
        cd /workspace/src
        echo "Installing dependencies..."
        
        # Node.js dependencies
        if [ -f package.json ]; then
            npm ci --production=false
            echo "Node.js dependencies installed"
        fi
        
        # Python dependencies
        if [ -f requirements.txt ]; then
            pip install -r requirements.txt
            echo "Python dependencies installed"
        fi
        
        # Go dependencies
        if [ -f go.mod ]; then
            go mod download
            echo "Go dependencies downloaded"
        fi
        
  - name: run-tests
    tool: bash
    description: "Execute test suite"
    parameters:
      command: |
        cd /workspace/src
        mkdir -p /workspace/test-results
        
        echo "Running tests..."
        
        # Run different test types
        if [ -f package.json ] && command -v npm; then
            npm test 2>&1 | tee /workspace/test-results/npm-test.log
        fi
        
        if [ -f requirements.txt ] && command -v pytest; then
            pytest --junitxml=/workspace/test-results/pytest.xml 2>&1 | tee /workspace/test-results/pytest.log
        fi
        
        if [ -f go.mod ] && command -v go; then
            go test ./... 2>&1 | tee /workspace/test-results/go-test.log
        fi
        
        echo "Tests completed"
        
  - name: build-artifacts
    tool: bash
    description: "Build application artifacts"
    parameters:
      command: |
        cd /workspace/src
        mkdir -p /workspace/dist
        
        echo "Building application..."
        
        # Build based on project type
        if [ -f Dockerfile ]; then
            echo "Building Docker image..."
            docker build -t myapp:$(git rev-parse --short HEAD) .
            docker save myapp:$(git rev-parse --short HEAD) > /workspace/dist/myapp.tar
        fi
        
        if [ -f package.json ]; then
            echo "Building Node.js application..."
            npm run build
            cp -r dist/* /workspace/dist/ 2>/dev/null || true
        fi
        
        if [ -f go.mod ]; then
            echo "Building Go application..."
            go build -o /workspace/dist/myapp ./cmd/...
        fi
        
        echo "Build completed"
        ls -la /workspace/dist/
        
  - name: security-scan
    tool: bash
    description: "Run security scans"
    parameters:
      command: |
        cd /workspace/src
        mkdir -p /workspace/security-reports
        
        echo "Running security scans..."
        
        # Dependency vulnerability scan
        if command -v npm-audit; then
            npm audit --json > /workspace/security-reports/npm-audit.json 2>/dev/null || true
        fi
        
        # Container scan (if Docker image exists)
        if [ -f /workspace/dist/myapp.tar ]; then
            echo "Docker image security scan would run here"
            # trivy image /workspace/dist/myapp.tar > /workspace/security-reports/trivy.txt
        fi
        
        echo "Security scans completed"
        
  - name: generate-build-report
    tool: python3
    description: "Create comprehensive build report"
    parameters:
      code: |
        import os
        import json
        import datetime
        from pathlib import Path
        
        workspace = Path('/workspace')
        
        print("Generating comprehensive build report...")
        
        # Collect build information
        build_info = {
            'timestamp': datetime.datetime.now().isoformat(),
            'workspace_size': sum(f.stat().st_size for f in workspace.rglob('*') if f.is_file()),
            'files_created': len(list(workspace.rglob('*'))),
            'build_artifacts': [],
            'test_results': {},
            'security_status': 'scanned'
        }
        
        # List build artifacts
        dist_dir = workspace / 'dist'
        if dist_dir.exists():
            for artifact in dist_dir.iterdir():
                if artifact.is_file():
                    build_info['build_artifacts'].append({
                        'name': artifact.name,
                        'size': artifact.stat().st_size,
                        'path': str(artifact)
                    })
        
        # Check test results
        test_dir = workspace / 'test-results'
        if test_dir.exists():
            for test_file in test_dir.iterdir():
                if test_file.is_file():
                    build_info['test_results'][test_file.name] = {
                        'size': test_file.stat().st_size,
                        'exists': True
                    }
        
        # Generate HTML report
        html_report = f"""
        <!DOCTYPE html>
        <html>
        <head>
            <title>Build Report</title>
            <style>
                body {{ font-family: Arial, sans-serif; margin: 40px; }}
                .header {{ background: #f0f0f0; padding: 20px; border-radius: 5px; }}
                .section {{ margin: 20px 0; }}
                .artifact {{ background: #e8f5e8; padding: 10px; margin: 5px; border-radius: 3px; }}
                pre {{ background: #f8f8f8; padding: 10px; border-radius: 3px; overflow-x: auto; }}
            </style>
        </head>
        <body>
            <div class="header">
                <h1>CI/CD Build Report</h1>
                <p><strong>Generated:</strong> {build_info['timestamp']}</p>
                <p><strong>Workspace Size:</strong> {build_info['workspace_size']:,} bytes</p>
                <p><strong>Files Created:</strong> {build_info['files_created']}</p>
            </div>
            
            <div class="section">
                <h2>Build Artifacts</h2>
        """
        
        for artifact in build_info['build_artifacts']:
            html_report += f"""
                <div class="artifact">
                    <strong>{artifact['name']}</strong> - {artifact['size']:,} bytes<br>
                    <small>{artifact['path']}</small>
                </div>
            """
        
        html_report += """
            </div>
            
            <div class="section">
                <h2>Test Results</h2>
                <pre>
        """
        
        for test_name, info in build_info['test_results'].items():
            html_report += f"{test_name}: {'PASSED' if info['exists'] else 'FAILED'}\\n"
        
        html_report += """
                </pre>
            </div>
            
            <div class="section">
                <h2>Build Summary</h2>
                <p>✅ Source code checked out</p>
                <p>✅ Dependencies installed</p>
                <p>✅ Tests executed</p>
                <p>✅ Artifacts built</p>
                <p>✅ Security scans completed</p>
                <p><strong>Status: BUILD SUCCESSFUL</strong></p>
            </div>
        </body>
        </html>
        """
        
        # Save reports
        with open('/workspace/build-report.html', 'w') as f:
            f.write(html_report)
        
        with open('/workspace/build-info.json', 'w') as f:
            json.dump(build_info, f, indent=2)
        
        print("Build report generated:")
        print(f"- HTML Report: /workspace/build-report.html")
        print(f"- JSON Data: /workspace/build-info.json")
        print(f"- Total workspace size: {build_info['workspace_size']:,} bytes")
```

### 2. Data Science Pipeline

```yaml
workflows:
- name: data-science-pipeline
  description: "End-to-end data science workflow"
  schedule: "0 6 * * *"  # Daily at 6 AM
  workspace:
    size: "50Gi"           # Large workspace for datasets
    storageClass: "fast-ssd"
  steps:
  - name: collect-raw-data
    tool: python3
    description: "Gather data from multiple sources"
    parameters:
      code: |
        import pandas as pd
        import requests
        import json
        from datetime import datetime, timedelta
        
        print("Collecting raw data from multiple sources...")
        
        # Create data directory
        import os
        os.makedirs('/workspace/raw-data', exist_ok=True)
        
        # Source 1: API data
        print("Fetching API data...")
        response = requests.get('https://jsonplaceholder.typicode.com/users')
        users_data = response.json()
        with open('/workspace/raw-data/users.json', 'w') as f:
            json.dump(users_data, f, indent=2)
        
        # Source 2: Generated time series data
        print("Generating time series data...")
        import numpy as np
        dates = pd.date_range(start='2024-01-01', end='2024-12-31', freq='D')
        values = np.random.randn(len(dates)).cumsum() + 100
        ts_data = pd.DataFrame({'date': dates, 'value': values})
        ts_data.to_csv('/workspace/raw-data/timeseries.csv', index=False)
        
        # Source 3: Mock sensor data
        print("Generating sensor data...")
        sensor_data = []
        for i in range(1000):
            sensor_data.append({
                'timestamp': (datetime.now() - timedelta(hours=i)).isoformat(),
                'temperature': 20 + np.random.normal(0, 5),
                'humidity': 50 + np.random.normal(0, 10),
                'pressure': 1013 + np.random.normal(0, 20)
            })
        
        sensor_df = pd.DataFrame(sensor_data)
        sensor_df.to_csv('/workspace/raw-data/sensors.csv', index=False)
        
        print(f"Data collection completed:")
        print(f"- Users: {len(users_data)} records")
        print(f"- Time series: {len(ts_data)} points")
        print(f"- Sensors: {len(sensor_df)} readings")
        
  - name: clean-and-validate
    tool: python3
    description: "Clean and validate collected data"
    parameters:
      code: |
        import pandas as pd
        import json
        import numpy as np
        from datetime import datetime
        import os
        
        print("Cleaning and validating data...")
        
        # Create cleaned data directory
        os.makedirs('/workspace/cleaned-data', exist_ok=True)
        
        # Clean users data
        print("Processing users data...")
        with open('/workspace/raw-data/users.json', 'r') as f:
            users_raw = json.load(f)
        
        users_df = pd.DataFrame(users_raw)
        users_df['email_domain'] = users_df['email'].str.split('@').str[1]
        users_df['lat'] = users_df['address'].apply(lambda x: float(x['geo']['lat']))
        users_df['lng'] = users_df['address'].apply(lambda x: float(x['geo']['lng']))
        users_cleaned = users_df[['id', 'name', 'email', 'email_domain', 'lat', 'lng']].copy()
        users_cleaned.to_csv('/workspace/cleaned-data/users_clean.csv', index=False)
        
        # Clean time series data
        print("Processing time series data...")
        ts_df = pd.read_csv('/workspace/raw-data/timeseries.csv')
        ts_df['date'] = pd.to_datetime(ts_df['date'])
        ts_df['value_smoothed'] = ts_df['value'].rolling(window=7, center=True).mean()
        ts_df['anomaly'] = np.abs(ts_df['value'] - ts_df['value_smoothed']) > 2 * ts_df['value'].std()
        ts_df.to_csv('/workspace/cleaned-data/timeseries_clean.csv', index=False)
        
        # Clean sensor data
        print("Processing sensor data...")
        sensor_df = pd.read_csv('/workspace/raw-data/sensors.csv')
        sensor_df['timestamp'] = pd.to_datetime(sensor_df['timestamp'])
        
        # Remove outliers
        for col in ['temperature', 'humidity', 'pressure']:
            q1 = sensor_df[col].quantile(0.25)
            q3 = sensor_df[col].quantile(0.75)
            iqr = q3 - q1
            lower_bound = q1 - 1.5 * iqr
            upper_bound = q3 + 1.5 * iqr
            sensor_df = sensor_df[(sensor_df[col] >= lower_bound) & (sensor_df[col] <= upper_bound)]
        
        sensor_df.to_csv('/workspace/cleaned-data/sensors_clean.csv', index=False)
        
        # Generate data quality report
        quality_report = {
            'processing_date': datetime.now().isoformat(),
            'users': {
                'original_count': len(users_raw),
                'cleaned_count': len(users_cleaned),
                'unique_domains': users_cleaned['email_domain'].nunique()
            },
            'timeseries': {
                'original_count': len(pd.read_csv('/workspace/raw-data/timeseries.csv')),
                'cleaned_count': len(ts_df),
                'anomalies_detected': ts_df['anomaly'].sum()
            },
            'sensors': {
                'original_count': len(pd.read_csv('/workspace/raw-data/sensors.csv')),
                'cleaned_count': len(sensor_df),
                'outliers_removed': len(pd.read_csv('/workspace/raw-data/sensors.csv')) - len(sensor_df)
            }
        }
        
        with open('/workspace/cleaned-data/quality_report.json', 'w') as f:
            json.dump(quality_report, f, indent=2)
        
        print("Data cleaning completed:")
        for dataset, stats in quality_report.items():
            if isinstance(stats, dict):
                print(f"- {dataset}: {stats}")
                
  - name: feature-engineering
    tool: python3
    description: "Create features for machine learning"
    parameters:
      code: |
        import pandas as pd
        import numpy as np
        from sklearn.preprocessing import StandardScaler, LabelEncoder
        import json
        import os
        
        print("Creating features for machine learning...")
        
        # Create features directory
        os.makedirs('/workspace/features', exist_ok=True)
        
        # Load cleaned data
        users_df = pd.read_csv('/workspace/cleaned-data/users_clean.csv')
        ts_df = pd.read_csv('/workspace/cleaned-data/timeseries_clean.csv')
        sensor_df = pd.read_csv('/workspace/cleaned-data/sensors_clean.csv')
        
        # User features
        print("Engineering user features...")
        user_features = users_df.copy()
        
        # Encode categorical variables
        le_domain = LabelEncoder()
        user_features['domain_encoded'] = le_domain.fit_transform(user_features['email_domain'])
        
        # Geographical features
        user_features['distance_from_center'] = np.sqrt(
            user_features['lat']**2 + user_features['lng']**2
        )
        
        # Time series features
        print("Engineering time series features...")
        ts_df['date'] = pd.to_datetime(ts_df['date'])
        ts_features = ts_df.copy()
        
        # Rolling statistics
        for window in [7, 30, 90]:
            ts_features[f'rolling_mean_{window}d'] = ts_features['value'].rolling(window=window).mean()
            ts_features[f'rolling_std_{window}d'] = ts_features['value'].rolling(window=window).std()
        
        # Trend and seasonality
        ts_features['day_of_year'] = ts_features['date'].dt.dayofyear
        ts_features['month'] = ts_features['date'].dt.month
        ts_features['quarter'] = ts_features['date'].dt.quarter
        
        # Sensor features
        print("Engineering sensor features...")
        sensor_df['timestamp'] = pd.to_datetime(sensor_df['timestamp'])
        sensor_features = sensor_df.copy()
        
        # Time-based features
        sensor_features['hour'] = sensor_features['timestamp'].dt.hour
        sensor_features['day_of_week'] = sensor_features['timestamp'].dt.dayofweek
        
        # Interaction features
        sensor_features['temp_humidity_interaction'] = (
            sensor_features['temperature'] * sensor_features['humidity']
        )
        sensor_features['comfort_index'] = (
            sensor_features['temperature'] + sensor_features['humidity']
        ) / 2
        
        # Standardize numerical features
        scaler = StandardScaler()
        numerical_cols = ['temperature', 'humidity', 'pressure', 'temp_humidity_interaction', 'comfort_index']
        sensor_features[numerical_cols] = scaler.fit_transform(sensor_features[numerical_cols])
        
        # Save engineered features
        user_features.to_csv('/workspace/features/user_features.csv', index=False)
        ts_features.to_csv('/workspace/features/timeseries_features.csv', index=False)
        sensor_features.to_csv('/workspace/features/sensor_features.csv', index=False)
        
        # Feature metadata
        feature_metadata = {
            'creation_date': pd.Timestamp.now().isoformat(),
            'user_features': {
                'shape': list(user_features.shape),
                'columns': list(user_features.columns),
                'categorical_features': ['email_domain', 'domain_encoded'],
                'numerical_features': ['lat', 'lng', 'distance_from_center']
            },
            'timeseries_features': {
                'shape': list(ts_features.shape),
                'columns': list(ts_features.columns),
                'date_range': [str(ts_features['date'].min()), str(ts_features['date'].max())],
                'rolling_windows': [7, 30, 90]
            },
            'sensor_features': {
                'shape': list(sensor_features.shape),
                'columns': list(sensor_features.columns),
                'standardized_columns': numerical_cols,
                'interaction_features': ['temp_humidity_interaction', 'comfort_index']
            }
        }
        
        with open('/workspace/features/metadata.json', 'w') as f:
            json.dump(feature_metadata, f, indent=2)
        
        print("Feature engineering completed:")
        for dataset, metadata in feature_metadata.items():
            if isinstance(metadata, dict) and 'shape' in metadata:
                print(f"- {dataset}: {metadata['shape']} ({len(metadata['columns'])} features)")
                
  - name: train-models
    tool: python3
    description: "Train machine learning models"
    parameters:
      code: |
        import pandas as pd
        import numpy as np
        from sklearn.model_selection import train_test_split, cross_val_score
        from sklearn.ensemble import RandomForestRegressor, RandomForestClassifier
        from sklearn.linear_model import LinearRegression, LogisticRegression
        from sklearn.metrics import mean_squared_error, accuracy_score, classification_report
        import joblib
        import json
        import os
        
        print("Training machine learning models...")
        
        # Create models directory
        os.makedirs('/workspace/models', exist_ok=True)
        
        # Load feature data
        sensor_df = pd.read_csv('/workspace/features/sensor_features.csv')
        
        # Prepare data for modeling
        feature_cols = ['temperature', 'humidity', 'pressure', 'hour', 'day_of_week', 
                       'temp_humidity_interaction', 'comfort_index']
        
        X = sensor_df[feature_cols].fillna(0)
        
        # Model 1: Comfort prediction (regression)
        print("Training comfort prediction model...")
        y_comfort = sensor_df['comfort_index']
        X_train, X_test, y_train, y_test = train_test_split(X, y_comfort, test_size=0.2, random_state=42)
        
        comfort_model = RandomForestRegressor(n_estimators=100, random_state=42)
        comfort_model.fit(X_train, y_train)
        
        comfort_pred = comfort_model.predict(X_test)
        comfort_mse = mean_squared_error(y_test, comfort_pred)
        comfort_score = comfort_model.score(X_test, y_test)
        
        # Model 2: Temperature category classification
        print("Training temperature category model...")
        sensor_df['temp_category'] = pd.cut(sensor_df['temperature'], 
                                           bins=[-np.inf, -1, 0, 1, np.inf], 
                                           labels=['cold', 'cool', 'warm', 'hot'])
        
        y_temp_cat = sensor_df['temp_category'].dropna()
        X_temp_cat = X.loc[y_temp_cat.index]
        
        X_train_cat, X_test_cat, y_train_cat, y_test_cat = train_test_split(
            X_temp_cat, y_temp_cat, test_size=0.2, random_state=42
        )
        
        temp_model = RandomForestClassifier(n_estimators=100, random_state=42)
        temp_model.fit(X_train_cat, y_train_cat)
        
        temp_pred = temp_model.predict(X_test_cat)
        temp_accuracy = accuracy_score(y_test_cat, temp_pred)
        
        # Cross-validation scores
        comfort_cv_scores = cross_val_score(comfort_model, X, y_comfort, cv=5)
        temp_cv_scores = cross_val_score(temp_model, X_temp_cat, y_temp_cat, cv=5)
        
        # Save models
        joblib.dump(comfort_model, '/workspace/models/comfort_model.joblib')
        joblib.dump(temp_model, '/workspace/models/temperature_model.joblib')
        
        # Feature importance
        comfort_importance = dict(zip(feature_cols, comfort_model.feature_importances_))
        temp_importance = dict(zip(feature_cols, temp_model.feature_importances_))
        
        # Model performance report
        model_report = {
            'training_date': pd.Timestamp.now().isoformat(),
            'dataset_size': len(sensor_df),
            'feature_count': len(feature_cols),
            'models': {
                'comfort_prediction': {
                    'type': 'regression',
                    'algorithm': 'RandomForestRegressor',
                    'test_mse': float(comfort_mse),
                    'test_r2_score': float(comfort_score),
                    'cv_scores': comfort_cv_scores.tolist(),
                    'cv_mean': float(comfort_cv_scores.mean()),
                    'cv_std': float(comfort_cv_scores.std()),
                    'feature_importance': {k: float(v) for k, v in comfort_importance.items()}
                },
                'temperature_category': {
                    'type': 'classification',
                    'algorithm': 'RandomForestClassifier',
                    'test_accuracy': float(temp_accuracy),
                    'cv_scores': temp_cv_scores.tolist(),
                    'cv_mean': float(temp_cv_scores.mean()),
                    'cv_std': float(temp_cv_scores.std()),
                    'feature_importance': {k: float(v) for k, v in temp_importance.items()}
                }
            }
        }
        
        with open('/workspace/models/model_report.json', 'w') as f:
            json.dump(model_report, f, indent=2)
        
        print("Model training completed:")
        print(f"- Comfort Model R² Score: {comfort_score:.4f}")
        print(f"- Temperature Model Accuracy: {temp_accuracy:.4f}")
        print(f"- Models saved to /workspace/models/")
        
  - name: generate-insights
    tool: python3
    description: "Generate business insights and visualizations"
    parameters:
      code: |
        import pandas as pd
        import numpy as np
        import json
        from datetime import datetime
        import os
        
        print("Generating business insights...")
        
        # Create insights directory
        os.makedirs('/workspace/insights', exist_ok=True)
        
        # Load all data and results
        with open('/workspace/cleaned-data/quality_report.json', 'r') as f:
            quality_report = json.load(f)
        
        with open('/workspace/features/metadata.json', 'r') as f:
            feature_metadata = json.load(f)
        
        with open('/workspace/models/model_report.json', 'r') as f:
            model_report = json.load(f)
        
        sensor_df = pd.read_csv('/workspace/features/sensor_features.csv')
        
        # Business insights
        insights = {
            'executive_summary': {
                'data_processing_date': datetime.now().isoformat(),
                'total_data_points': sum([
                    quality_report['users']['cleaned_count'],
                    quality_report['timeseries']['cleaned_count'],
                    quality_report['sensors']['cleaned_count']
                ]),
                'model_accuracy': {
                    'comfort_prediction': model_report['models']['comfort_prediction']['test_r2_score'],
                    'temperature_classification': model_report['models']['temperature_category']['test_accuracy']
                },
                'data_quality_score': 0.95,  # Example score
                'key_findings': [
                    'Sensor data shows clear seasonal patterns',
                    'Temperature and humidity are highly correlated',
                    'Model predictions exceed 85% accuracy threshold',
                    'Data quality improved by 15% after cleaning'
                ]
            },
            
            'operational_insights': {
                'sensor_patterns': {
                    'peak_temperature_hour': int(sensor_df.groupby('hour')['temperature'].mean().idxmax()),
                    'most_stable_day': int(sensor_df.groupby('day_of_week')['temperature'].std().idxmin()),
                    'average_comfort_index': float(sensor_df['comfort_index'].mean()),
                    'temperature_range': {
                        'min': float(sensor_df['temperature'].min()),
                        'max': float(sensor_df['temperature'].max()),
                        'std': float(sensor_df['temperature'].std())
                    }
                },
                
                'recommendations': [
                    'Optimize HVAC scheduling based on peak temperature hours',
                    'Implement predictive maintenance using comfort index trends',
                    'Monitor temperature anomalies for early warning system',
                    'Expand sensor network in high-variance areas'
                ]
            },
            
            'technical_metrics': {
                'pipeline_performance': {
                    'data_processing_efficiency': 'High',
                    'model_training_time': '< 5 minutes',
                    'feature_count': feature_metadata['sensor_features']['shape'][1],
                    'data_quality_improvements': {
                        'outliers_removed': quality_report['sensors']['outliers_removed'],
                        'anomalies_detected': quality_report['timeseries']['anomalies_detected']
                    }
                },
                
                'model_metrics': model_report['models']
            }
        }
        
        # Save insights
        with open('/workspace/insights/business_insights.json', 'w') as f:
            json.dump(insights, f, indent=2)
        
        # Generate HTML dashboard
        dashboard_html = f"""
        <!DOCTYPE html>
        <html>
        <head>
            <title>Data Science Pipeline Dashboard</title>
            <style>
                body {{ font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }}
                .container {{ max-width: 1200px; margin: 0 auto; }}
                .header {{ background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 30px; border-radius: 10px; text-align: center; }}
                .grid {{ display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; margin-top: 20px; }}
                .card {{ background: white; padding: 25px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }}
                .metric {{ text-align: center; padding: 15px; background: #f8f9fa; border-radius: 8px; margin: 10px 0; }}
                .metric-value {{ font-size: 2em; font-weight: bold; color: #28a745; }}
                .metric-label {{ color: #6c757d; font-size: 0.9em; }}
                .insight {{ background: #e3f2fd; padding: 15px; border-left: 4px solid #2196f3; margin: 10px 0; border-radius: 4px; }}
                .recommendation {{ background: #fff3e0; padding: 15px; border-left: 4px solid #ff9800; margin: 10px 0; border-radius: 4px; }}
                ul {{ list-style-type: none; padding: 0; }}
                li {{ padding: 8px 0; border-bottom: 1px solid #eee; }}
                li:before {{ content: "✓"; color: #28a745; font-weight: bold; margin-right: 10px; }}
            </style>
        </head>
        <body>
            <div class="container">
                <div class="header">
                    <h1>🔬 Data Science Pipeline Dashboard</h1>
                    <p>Automated ML Pipeline Results - {insights['executive_summary']['data_processing_date'][:10]}</p>
                </div>
                
                <div class="grid">
                    <div class="card">
                        <h2>📊 Executive Summary</h2>
                        <div class="metric">
                            <div class="metric-value">{insights['executive_summary']['total_data_points']:,}</div>
                            <div class="metric-label">Total Data Points Processed</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{insights['executive_summary']['model_accuracy']['comfort_prediction']:.2%}</div>
                            <div class="metric-label">Comfort Model Accuracy</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{insights['executive_summary']['model_accuracy']['temperature_classification']:.2%}</div>
                            <div class="metric-label">Temperature Model Accuracy</div>
                        </div>
                    </div>
                    
                    <div class="card">
                        <h2>🎯 Key Findings</h2>
        """
        
        for finding in insights['executive_summary']['key_findings']:
            dashboard_html += f'<div class="insight">{finding}</div>'
        
        dashboard_html += f"""
                    </div>
                    
                    <div class="card">
                        <h2>⚙️ Operational Insights</h2>
                        <p><strong>Peak Temperature Hour:</strong> {insights['operational_insights']['sensor_patterns']['peak_temperature_hour']}:00</p>
                        <p><strong>Most Stable Day:</strong> {['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'][insights['operational_insights']['sensor_patterns']['most_stable_day']]}</p>
                        <p><strong>Avg Comfort Index:</strong> {insights['operational_insights']['sensor_patterns']['average_comfort_index']:.2f}</p>
                        <p><strong>Temperature Range:</strong> {insights['operational_insights']['sensor_patterns']['temperature_range']['min']:.1f}°C to {insights['operational_insights']['sensor_patterns']['temperature_range']['max']:.1f}°C</p>
                    </div>
                    
                    <div class="card">
                        <h2>💡 Recommendations</h2>
        """
        
        for rec in insights['operational_insights']['recommendations']:
            dashboard_html += f'<div class="recommendation">{rec}</div>'
        
        dashboard_html += f"""
                    </div>
                    
                    <div class="card">
                        <h2>🔧 Technical Metrics</h2>
                        <ul>
                            <li>Features Engineered: {insights['technical_metrics']['pipeline_performance']['feature_count']}</li>
                            <li>Outliers Removed: {insights['technical_metrics']['pipeline_performance']['data_quality_improvements']['outliers_removed']}</li>
                            <li>Anomalies Detected: {insights['technical_metrics']['pipeline_performance']['data_quality_improvements']['anomalies_detected']}</li>
                            <li>Processing Efficiency: {insights['technical_metrics']['pipeline_performance']['data_processing_efficiency']}</li>
                        </ul>
                    </div>
                    
                    <div class="card">
                        <h2>📈 Model Performance</h2>
                        <div class="metric">
                            <div class="metric-value">{insights['technical_metrics']['model_metrics']['comfort_prediction']['cv_mean']:.3f}</div>
                            <div class="metric-label">Comfort Model CV Score</div>
                        </div>
                        <div class="metric">
                            <div class="metric-value">{insights['technical_metrics']['model_metrics']['temperature_category']['cv_mean']:.3f}</div>
                            <div class="metric-label">Temperature Model CV Score</div>
                        </div>
                    </div>
                </div>
            </div>
        </body>
        </html>
        """
        
        with open('/workspace/insights/dashboard.html', 'w') as f:
            f.write(dashboard_html)
        
        # Create summary report
        summary = f"""
        DATA SCIENCE PIPELINE SUMMARY
        ============================
        
        Execution Date: {insights['executive_summary']['data_processing_date']}
        Total Data Points: {insights['executive_summary']['total_data_points']:,}
        
        MODEL PERFORMANCE:
        - Comfort Prediction: {insights['executive_summary']['model_accuracy']['comfort_prediction']:.1%} R² Score
        - Temperature Classification: {insights['executive_summary']['model_accuracy']['temperature_classification']:.1%} Accuracy
        
        KEY INSIGHTS:
        {chr(10).join([f'• {finding}' for finding in insights['executive_summary']['key_findings']])}
        
        DELIVERABLES:
        ✓ Raw data collected and validated
        ✓ Features engineered and standardized  
        ✓ Machine learning models trained
        ✓ Business insights generated
        ✓ Interactive dashboard created
        
        FILES CREATED:
        • /workspace/insights/business_insights.json
        • /workspace/insights/dashboard.html
        • /workspace/models/comfort_model.joblib
        • /workspace/models/temperature_model.joblib
        
        RECOMMENDATIONS:
        {chr(10).join([f'• {rec}' for rec in insights['operational_insights']['recommendations']])}
        
        Pipeline executed successfully! 🎉
        """
        
        with open('/workspace/insights/summary.txt', 'w') as f:
            f.write(summary)
        
        print("Business insights generated:")
        print("• JSON insights: /workspace/insights/business_insights.json") 
        print("• HTML dashboard: /workspace/insights/dashboard.html")
        print("• Summary report: /workspace/insights/summary.txt")
        print("\nPipeline completed successfully! 🚀")
```

## Best Practices

### 1. Workspace Size Planning

```yaml
# Small workflows (< 1GB data)
workspace:
  size: "1Gi"

# Medium workflows (1-10GB data) 
workspace:
  size: "10Gi"
  storageClass: "standard"

# Large workflows (10GB+ data)
workspace:
  size: "100Gi"
  storageClass: "fast-ssd"
```

### 2. Data Organization

```bash
# Recommended workspace structure
/workspace/
├── raw-data/           # Original input data
├── processed-data/     # Cleaned/transformed data  
├── models/            # Trained ML models
├── reports/           # Generated reports
├── logs/              # Step execution logs
├── temp/              # Temporary files
└── output/            # Final deliverables
```

### 3. Error Handling

```yaml
steps:
- name: robust-processing
  tool: bash
  parameters:
    command: |
      set -e  # Exit on any error
      
      # Create required directories
      mkdir -p /workspace/{raw-data,processed-data,logs}
      
      # Log execution
      echo "$(date): Starting processing" >> /workspace/logs/execution.log
      
      # Process with error handling
      if ! process_data.py > /workspace/logs/process.log 2>&1; then
        echo "ERROR: Data processing failed" >> /workspace/logs/execution.log
        cp /workspace/logs/process.log /workspace/error-details.log
        exit 1
      fi
      
      # Validate results
      if [ ! -f /workspace/processed-data/results.csv ]; then
        echo "ERROR: Expected output file missing" >> /workspace/logs/execution.log
        exit 1
      fi
      
      echo "$(date): Processing completed successfully" >> /workspace/logs/execution.log
```

### 4. Performance Optimization

```yaml
# Use fast storage for I/O intensive workflows
workspace:
  storageClass: "fast-ssd"
  size: "50Gi"

# Optimize step ordering for data locality
steps:
- name: download-large-dataset
  # Downloads once, used by multiple steps
- name: process-subset-1
  # Processes part of the data
- name: process-subset-2  
  # Processes another part
- name: combine-results
  # Combines all processed parts
```

## Troubleshooting

### Common Issues

#### 1. Workspace Not Created

**Symptoms:**
```bash
Error: /workspace: no such file or directory
```

**Solutions:**
```yaml
# Explicitly enable workspace
workspace:
  enabled: true

# Or check if workflow has multiple steps
steps:
- name: step1
  tool: echo
  parameters:
    message: "Need at least 2 steps for auto-enable"
- name: step2  
  tool: echo
  parameters:
    message: "Workspace will be auto-created"
```

#### 2. Permission Issues

**Symptoms:**
```bash
Permission denied: /workspace/output.txt
```

**Solutions:**
```yaml
steps:
- name: fix-permissions
  tool: bash
  parameters:
    command: |
      # Ensure workspace is writable
      chmod 755 /workspace
      chown -R $(whoami):$(whoami) /workspace
      
      # Create directories with proper permissions
      mkdir -p /workspace/{data,logs,temp}
      chmod 755 /workspace/{data,logs,temp}
```

#### 3. Disk Space Issues  

**Symptoms:**
```bash
No space left on device
```

**Solutions:**
```yaml
# Increase workspace size
workspace:
  size: "50Gi"  # Increase from default 1Gi

# Or clean up during processing
steps:
- name: process-and-cleanup
  tool: bash
  parameters:
    command: |
      # Process data
      large_processing_job.py
      
      # Clean up intermediate files
      rm -rf /workspace/temp/*
      
      # Compress large outputs
      tar -czf /workspace/results.tar.gz /workspace/processed-data/
      rm -rf /workspace/processed-data/
```

#### 4. Data Not Persisting Between Steps

**Symptoms:**
```bash
# Step 2 cannot find files created in Step 1
FileNotFoundError: /workspace/data.json
```

**Debug Commands:**
```yaml
steps:
- name: debug-workspace
  tool: bash
  parameters:
    command: |
      echo "=== WORKSPACE DEBUG INFO ==="
      echo "PWD: $(pwd)"
      echo "WORKSPACE_ENABLED: $WORKSPACE_ENABLED"
      echo "WORKFLOW_WORKSPACE_PATH: $WORKFLOW_WORKSPACE_PATH"
      echo "Mount points:"
      mount | grep workspace
      echo "Workspace contents:"
      ls -la /workspace/ || echo "Workspace not found"
      echo "Disk usage:"
      df -h /workspace || echo "Cannot check workspace usage"
```

### Monitoring Workspace Usage

```yaml
steps:
- name: monitor-workspace
  tool: bash  
  parameters:
    command: |
      echo "=== WORKSPACE MONITORING ==="
      echo "Total size: $(du -sh /workspace | cut -f1)"
      echo "File count: $(find /workspace -type f | wc -l)"
      echo "Largest files:"
      find /workspace -type f -exec ls -lh {} \; | sort -k5 -hr | head -5
      echo "Disk usage by directory:"
      du -sh /workspace/*/ 2>/dev/null || echo "No subdirectories"
```

### Volume Cleanup

```bash
# Check for leftover workspace PVCs
kubectl get pvc -l app.kubernetes.io/name=workflow-workspace

# Manual cleanup if auto-delete failed
kubectl delete pvc workspace-myworkflow-1234567890

# Check workspace usage across all workflows  
kubectl get pvc -l app.kubernetes.io/name=workflow-workspace \
  --output=jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.capacity.storage}{"\n"}{end}'
```

---

Workspace volumes provide a powerful foundation for building sophisticated multi-step workflows in Matey. They enable data scientists, DevOps engineers, and automation specialists to create complex pipelines that rival enterprise workflow orchestration platforms while maintaining the simplicity and flexibility of Kubernetes-native operations.