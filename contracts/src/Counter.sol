// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

/// @title Counter
/// @notice Demo target contract for SmartAccount interactions.
///         Each caller (smart account) has its own independent counter.
contract Counter {
    /// @notice Per-account counters. Keyed by msg.sender (the smart account address
    ///         when called via SmartAccount.execute).
    mapping(address => uint256) private _counts;

    /// @notice Increment the caller's counter by one.
    function increment() external {
        _counts[msg.sender]++;
    }

    /// @notice Set the caller's counter to an arbitrary value.
    /// @param newNumber The value to set.
    function setNumber(uint256 newNumber) external {
        _counts[msg.sender] = newNumber;
    }

    /// @notice Returns the counter value for a specific account.
    /// @param account The address to query.
    /// @return The current count for that account.
    function getCount(address account) external view returns (uint256) {
        return _counts[account];
    }
}
